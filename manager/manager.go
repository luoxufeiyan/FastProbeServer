package manager

import (
	"log"
	"sync"
	"time"

	"FastProbeServer/config"
	"FastProbeServer/db"
)

type NodeReport struct {
	CPU       float64
	MemUsed   int64
	MemTotal  int64
	NetRx     int64
	NetTx     int64
	DiskUsed  int64
	DiskTotal int64
	Uptime    int64
	RecordedAt time.Time
}

// NodeStatus represents the current state of a node
type NodeStatus struct {
	db.Node
	IsOnline   bool    `json:"is_online"`
	OS         string  `json:"os"`
	KernelVer  string  `json:"kernel_version"`
	CPU        float64 `json:"cpu"`
	MemUsed    int64   `json:"mem_used"`
	MemTotal   int64   `json:"mem_total"`
	SwapUsed   int64   `json:"swap_used"`
	SwapTotal  int64   `json:"swap_total"`
	NetRx             int64     `json:"net_rx"`      // current rx rate (bytes/s)
	NetTx             int64     `json:"net_tx"`      // current tx rate (bytes/s)
	NetTotalRx        int64     `json:"net_total_rx"`
	NetTotalTx        int64     `json:"net_total_tx"`
	LastNetTotalRx    int64     `json:"-"`
	LastNetTotalTx    int64     `json:"-"`
	TotalTrafficRx    int64     `json:"total_traffic_rx"`
	TotalTrafficTx    int64     `json:"total_traffic_tx"`
	MonthTrafficRx    int64     `json:"month_traffic_rx"`
	MonthTrafficTx    int64     `json:"month_traffic_tx"`
	TrafficResetMonth string    `json:"traffic_reset_month"`
	DiskUsed          int64     `json:"disk_used"`
	DiskTotal         int64     `json:"disk_total"`
	Uptime     int64   `json:"uptime"`
	IP         string  `json:"ip"`
	IPStack    string  `json:"ip_stack"`
	Version    string  `json:"version"`
	LastUpdate time.Time `json:"last_update"`
	OfflineTime string `json:"offline_time,omitempty"`
	RecentReports []NodeReport `json:"-"`
}

var (
	// statuses maps NodeID to NodeStatus
	statuses = make(map[int]*NodeStatus)
	// secretMap maps Node Secret to NodeID for fast auth
	secretMap = make(map[string]int)
	stateLock sync.RWMutex

	// pendingNodes maps secret to PendingNode
	pendingNodes = make(map[string]PendingNode)
	pendingLock  sync.RWMutex

	stopChan chan struct{}
)

// PendingNode represents an unverified node trying to connect
type PendingNode struct {
	Secret   string    `json:"secret"`
	IP       string    `json:"ip"`
	Hostname string    `json:"hostname"`
	OS       string    `json:"os"`
	LastSeen time.Time `json:"last_seen"`
}

// RecordPendingNode saves an unverified node's attempt
func RecordPendingNode(secret, ip, hostname, os string) {
	pendingLock.Lock()
	defer pendingLock.Unlock()
	pendingNodes[secret] = PendingNode{
		Secret:   secret,
		IP:       ip,
		Hostname: hostname,
		OS:       os,
		LastSeen: time.Now(),
	}
}

// GetPendingNodes returns all pending node requests
func GetPendingNodes() []PendingNode {
	pendingLock.RLock()
	defer pendingLock.RUnlock()

	var res []PendingNode
	for _, p := range pendingNodes {
		res = append(res, p)
	}
	return res
}

// RemovePendingNode removes a pending node request
func RemovePendingNode(secret string) {
	pendingLock.Lock()
	defer pendingLock.Unlock()
	delete(pendingNodes, secret)
}

// ReloadNodes fetches nodes from the DB and initializes/updates the memory structures
func ReloadNodes() error {
	nodes, err := db.GetAllNodes()
	if err != nil {
		return err
	}

	lastTimes, err := db.GetLastSnapshotTimes()
	if err != nil {
		log.Printf("Warning: failed to fetch last snapshot times: %v", err)
		lastTimes = make(map[int]time.Time)
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
			existing.Config = n.Config
		} else {
			statuses[n.ID] = &NodeStatus{
				Node:              n,
				IsOnline:          false,
				TotalTrafficRx:    n.Config.TotalTrafficRx,
				TotalTrafficTx:    n.Config.TotalTrafficTx,
				MonthTrafficRx:    n.Config.MonthTrafficRx,
				MonthTrafficTx:    n.Config.MonthTrafficTx,
				TrafficResetMonth: n.Config.TrafficResetMonth,
			}
			if t, ok := lastTimes[n.ID]; ok {
				statuses[n.ID].OfflineTime = t.Format(time.RFC3339)
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
func UpdateReport(nodeID int, osStr, kernel string, cpu float64, memUsed, memTotal, swapUsed, swapTotal, netRx, netTx, netTotalRx, netTotalTx, diskUsed, diskTotal, uptime int64, ip, ipStack, version string) {
	stateLock.Lock()
	defer stateLock.Unlock()

	if status, ok := statuses[nodeID]; ok {
		status.IsOnline = true
		if osStr != "" {
			status.OS = osStr
		}
		if kernel != "" {
			status.KernelVer = kernel
		}
		status.CPU = cpu
		status.MemUsed = memUsed
		status.MemTotal = memTotal
		status.SwapUsed = swapUsed
		status.SwapTotal = swapTotal
		status.NetRx = netRx
		status.NetTx = netTx
		
		currentMonth := time.Now().Format("2006-01")
		if status.TrafficResetMonth != currentMonth {
			status.MonthTrafficRx = 0
			status.MonthTrafficTx = 0
			status.TrafficResetMonth = currentMonth
		}

		if status.LastNetTotalRx > 0 && netTotalRx >= status.LastNetTotalRx {
			diffRx := netTotalRx - status.LastNetTotalRx
			status.TotalTrafficRx += diffRx
			status.MonthTrafficRx += diffRx
		} else if netTotalRx > 0 {
			// Rebooted or first report
			status.TotalTrafficRx += netTotalRx
			status.MonthTrafficRx += netTotalRx
		}

		if status.LastNetTotalTx > 0 && netTotalTx >= status.LastNetTotalTx {
			diffTx := netTotalTx - status.LastNetTotalTx
			status.TotalTrafficTx += diffTx
			status.MonthTrafficTx += diffTx
		} else if netTotalTx > 0 {
			status.TotalTrafficTx += netTotalTx
			status.MonthTrafficTx += netTotalTx
		}

		status.LastNetTotalRx = netTotalRx
		status.LastNetTotalTx = netTotalTx
		
		status.NetTotalRx = netTotalRx
		status.NetTotalTx = netTotalTx
		status.DiskUsed = diskUsed
		status.DiskTotal = diskTotal
		status.Uptime = uptime
		if ip != "" {
			status.IP = ip
		}
		if ipStack != "" {
			status.IPStack = ipStack
		}
		if version != "" {
			status.Version = version
		}
		status.LastUpdate = time.Now()

		status.RecentReports = append(status.RecentReports, NodeReport{
			CPU:        cpu,
			MemUsed:    memUsed,
			MemTotal:   memTotal,
			NetRx:      netRx,
			NetTx:      netTx,
			DiskUsed:   diskUsed,
			DiskTotal:  diskTotal,
			Uptime:     uptime,
			RecordedAt: status.LastUpdate,
		})
		if len(status.RecentReports) > 10 {
			status.RecentReports = status.RecentReports[1:]
		}
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

// GetReportInterval returns the configured interval for a node, or default global interval
func GetReportInterval(nodeID int) int {
	stateLock.RLock()
	defer stateLock.RUnlock()

	if status, ok := statuses[nodeID]; ok {
		if status.Config.ReportInterval > 0 {
			return status.Config.ReportInterval
		}
	}
	if config.Current != nil && config.Current.GlobalReportInterval > 0 {
		return config.Current.GlobalReportInterval
	}
	return 10
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

// checkOfflineNodes marks nodes as offline if not updated in 3 intervals
func checkOfflineNodes() {
	stateLock.Lock()
	defer stateLock.Unlock()

	now := time.Now()

	globalInterval := 10
	if config.Current != nil && config.Current.GlobalReportInterval > 0 {
		globalInterval = config.Current.GlobalReportInterval
	}

	for _, status := range statuses {
		interval := status.Config.ReportInterval
		if interval <= 0 {
			interval = globalInterval
		}
		threshold := time.Duration(interval * 3) * time.Second

		if status.IsOnline && now.Sub(status.LastUpdate) > threshold {
			status.IsOnline = false
			status.OfflineTime = now.Format(time.RFC3339)

			for _, r := range status.RecentReports {
				err := db.RecordSnapshotWithTime(id, r.CPU, r.MemUsed, r.MemTotal, r.NetRx, r.NetTx, r.DiskUsed, r.DiskTotal, r.Uptime, r.RecordedAt)
				if err != nil {
					log.Printf("Error recording offline snapshot for node %d: %v", id, err)
				}
			}
			status.RecentReports = nil
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

		// Persist the traffic data back to DB Config
		status.Config.TotalTrafficRx = status.TotalTrafficRx
		status.Config.TotalTrafficTx = status.TotalTrafficTx
		status.Config.MonthTrafficRx = status.MonthTrafficRx
		status.Config.MonthTrafficTx = status.MonthTrafficTx
		status.Config.TrafficResetMonth = status.TrafficResetMonth
		
		go func(nid int, cfg db.NodeConfig) {
			db.UpdateNodeConfig(nid, cfg)
		}(id, status.Config)
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
