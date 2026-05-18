package manager

import (
	"log"
	"sync"
	"time"

	"FastProbeServer/config"
	"FastProbeServer/db"
)

// NodeStatus represents the current state of a node
type NodeStatus struct {
	db.Node
	IsOnline   bool    `json:"is_online"`
	CPU        float64 `json:"cpu"`
	MemUsed    int64   `json:"mem_used"`
	MemTotal   int64   `json:"mem_total"`
	NetRx      int64   `json:"net_rx"`      // current rx rate (bytes/s)
	NetTx      int64   `json:"net_tx"`      // current tx rate (bytes/s)
	DiskUsed   int64   `json:"disk_used"`
	DiskTotal  int64   `json:"disk_total"`
	Uptime     int64   `json:"uptime"`
	LastUpdate time.Time `json:"last_update"`
}

var (
	// statuses maps NodeID to NodeStatus
	statuses = make(map[int]*NodeStatus)
	// secretMap maps Node Secret to NodeID for fast auth
	secretMap = make(map[string]int)
	stateLock sync.RWMutex

	stopChan chan struct{}
)

// ReloadNodes fetches nodes from the DB and initializes/updates the memory structures
func ReloadNodes() error {
	nodes, err := db.GetAllNodes()
	if err != nil {
		return err
	}

	stateLock.Lock()
	defer stateLock.Unlock()

	// Rebuild secret map and update statuses
	newSecretMap := make(map[string]int)
	for _, n := range nodes {
		newSecretMap[n.Secret] = n.ID

		if existing, ok := statuses[n.ID]; ok {
			// Update basic info
			existing.Name = n.Name
			existing.Location = n.Location
			existing.Secret = n.Secret
			existing.IsAdminOnly = n.IsAdminOnly
		} else {
			statuses[n.ID] = &NodeStatus{
				Node:       n,
				IsOnline:   false,
			}
		}
	}
	
	// Remove nodes that were deleted from DB
	for id := range statuses {
		found := false
		for _, n := range nodes {
			if n.ID == id {
				found = true
				break
			}
		}
		if !found {
			delete(statuses, id)
		}
	}

	secretMap = newSecretMap
	return nil
}

// Authenticate checks if a secret is valid and returns the NodeID
func Authenticate(secret string) (int, bool) {
	stateLock.RLock()
	defer stateLock.RUnlock()

	id, ok := secretMap[secret]
	return id, ok
}

// UpdateReport receives a new report from a node and updates memory state
func UpdateReport(nodeID int, cpu float64, memUsed, memTotal, netRx, netTx, diskUsed, diskTotal, uptime int64) {
	stateLock.Lock()
	defer stateLock.Unlock()

	if status, ok := statuses[nodeID]; ok {
		status.IsOnline = true
		status.CPU = cpu
		status.MemUsed = memUsed
		status.MemTotal = memTotal
		status.NetRx = netRx
		status.NetTx = netTx
		status.DiskUsed = diskUsed
		status.DiskTotal = diskTotal
		status.Uptime = uptime
		status.LastUpdate = time.Now()
	}
}

// GetAllStatuses returns a copy of all current statuses
func GetAllStatuses() []NodeStatus {
	stateLock.RLock()
	defer stateLock.RUnlock()

	res := make([]NodeStatus, 0, len(statuses))
	for _, s := range statuses {
		res = append(res, *s)
	}
	return res
}

// StartBackgroundTasks starts the goroutines for checking offline nodes and taking snapshots
func StartBackgroundTasks() {
	if stopChan != nil {
		close(stopChan) // Stop existing if any
	}
	stopChan = make(chan struct{})

	go func() {
		checkTicker := time.NewTicker(15 * time.Second)
		defer checkTicker.Stop()

		var snapshotTicker *time.Ticker
		if config.Current != nil && config.Current.SnapshotMins > 0 {
			snapshotTicker = time.NewTicker(time.Duration(config.Current.SnapshotMins) * time.Minute)
		} else {
			snapshotTicker = time.NewTicker(5 * time.Minute) // Default
		}
		defer snapshotTicker.Stop()

		for {
			select {
			case <-checkTicker.C:
				checkOfflineNodes()
			case <-snapshotTicker.C:
				if config.Current != nil && config.Current.IsSetup {
					takeSnapshots()
				}
			case <-stopChan:
				return
			}
		}
	}()
}

// checkOfflineNodes marks nodes as offline if not updated in 15 seconds
func checkOfflineNodes() {
	stateLock.Lock()
	defer stateLock.Unlock()

	now := time.Now()
	for _, status := range statuses {
		if status.IsOnline && now.Sub(status.LastUpdate) > 15*time.Second {
			status.IsOnline = false
		}
	}
}

// takeSnapshots records the current state of online nodes into the DB history
func takeSnapshots() {
	stateLock.RLock()
	// Deep copy needed stats to avoid holding lock during DB inserts
	type snapshotData struct {
		nodeID                                                       int
		cpu                                                          float64
		memUsed, memTotal, netRx, netTx, diskUsed, diskTotal, uptime int64
	}
	
	var snaps []snapshotData
	for id, status := range statuses {
		// Only snapshot if they are online or just went offline
		snaps = append(snaps, snapshotData{
			nodeID:    id,
			cpu:       status.CPU,
			memUsed:   status.MemUsed,
			memTotal:  status.MemTotal,
			netRx:     status.NetRx,
			netTx:     status.NetTx,
			diskUsed:  status.DiskUsed,
			diskTotal: status.DiskTotal,
			uptime:    status.Uptime,
		})
	}
	stateLock.RUnlock()

	for _, s := range snaps {
		err := db.RecordSnapshot(s.nodeID, s.cpu, s.memUsed, s.memTotal, s.netRx, s.netTx, s.diskUsed, s.diskTotal, s.uptime)
		if err != nil {
			log.Printf("Error recording snapshot for node %d: %v", s.nodeID, err)
		}
	}
}

// StopBackgroundTasks stops the background ticker goroutines
func StopBackgroundTasks() {
	if stopChan != nil {
		close(stopChan)
		stopChan = nil
	}
}
