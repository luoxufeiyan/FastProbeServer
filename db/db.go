package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"FastProbeServer/config"

	_ "github.com/go-sql-driver/mysql"
)

var DB *sql.DB

// NodeConfig stores JSON-encoded settings for a node
type NodeConfig struct {
	ReportInterval int      `json:"report_interval"`
	ShowDetails    bool     `json:"show_details"`
	ShowIP         bool     `json:"show_ip"`
	Tags           []string `json:"tags"`

	// Accumulated traffic
	TotalTrafficRx    int64  `json:"total_traffic_rx,omitempty"`
	TotalTrafficTx    int64  `json:"total_traffic_tx,omitempty"`
	MonthTrafficRx    int64  `json:"month_traffic_rx,omitempty"`
	MonthTrafficTx    int64  `json:"month_traffic_tx,omitempty"`
	TrafficResetMonth string `json:"traffic_reset_month,omitempty"`
}

// InitDB initializes the database connection and creates tables if they don't exist
func InitDB() error {
	cfgLock := config.Current
	if cfgLock == nil || !cfgLock.IsSetup {
		return fmt.Errorf("config not setup")
	}

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		cfgLock.DBUser,
		cfgLock.DBPassword,
		cfgLock.DBHost,
		cfgLock.DBPort,
		cfgLock.DBName,
	)

	var err error
	DB, err = sql.Open("mysql", dsn)
	if err != nil {
		return err
	}

	DB.SetMaxOpenConns(50)
	DB.SetMaxIdleConns(10)
	DB.SetConnMaxLifetime(time.Hour)

	if err := DB.Ping(); err != nil {
		return err
	}

	return createTables()
}

// createTables executes the table creation SQL statements
func createTables() error {
	prefix := config.Current.DBPrefix

	// Nodes Table
	// We store Node ID, Name, Location, Secret
	nodesTable := fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS %snodes (
		id INT AUTO_INCREMENT PRIMARY KEY,
		name VARCHAR(100) NOT NULL,
		location VARCHAR(100) DEFAULT '',
		secret VARCHAR(255) NOT NULL UNIQUE,
		config JSON,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		is_admin_only BOOLEAN DEFAULT FALSE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;`, prefix)

	// History Table
	// Stores historical snapshots of stats
	historyTable := fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS %shistory (
		id BIGINT AUTO_INCREMENT PRIMARY KEY,
		node_id INT NOT NULL,
		cpu_usage DOUBLE,
		mem_used BIGINT,
		mem_total BIGINT,
		net_rx_bytes BIGINT,
		net_tx_bytes BIGINT,
		disk_used BIGINT,
		disk_total BIGINT,
		uptime BIGINT,
		recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		INDEX(node_id),
		INDEX(recorded_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;`, prefix)

	_, err := DB.Exec(nodesTable)
	if err != nil {
		log.Printf("Error creating nodes table: %v", err)
		return err
	}

	// Try to add the config column for users who upgrade from previous versions without dropping tables
	alterCmd := fmt.Sprintf("ALTER TABLE %snodes ADD COLUMN config JSON", prefix)
	DB.Exec(alterCmd)

	_, err = DB.Exec(historyTable)
	if err != nil {
		log.Printf("Error creating history table: %v", err)
		return err
	}

	eventsTable := fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS %snode_events (
		id BIGINT AUTO_INCREMENT PRIMARY KEY,
		node_id INT NOT NULL,
		event VARCHAR(50) NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		INDEX(node_id),
		INDEX(created_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;`, prefix)

	_, err = DB.Exec(eventsTable)
	if err != nil {
		log.Printf("Error creating events table: %v", err)
		return err
	}

	return nil
}

// Node represents a node configuration in the database
type Node struct {
	ID          int        `json:"id"`
	Name        string     `json:"name"`
	Location    string     `json:"location"`
	Secret      string     `json:"secret"`
	IsAdminOnly bool       `json:"is_admin_only"`
	Config      NodeConfig `json:"config"`
}

// GetAllNodes retrieves all nodes from the database
func GetAllNodes() ([]Node, error) {
	if DB == nil {
		return nil, fmt.Errorf("db not initialized")
	}

	query := fmt.Sprintf("SELECT id, name, location, secret, is_admin_only, config FROM %snodes", config.Current.DBPrefix)
	rows, err := DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		var configBytes []byte
		if err := rows.Scan(&n.ID, &n.Name, &n.Location, &n.Secret, &n.IsAdminOnly, &configBytes); err != nil {
			return nil, err
		}
		if string(configBytes) != "" {
			json.Unmarshal(configBytes, &n.Config)
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// GetLastSnapshotTimes retrieves the latest recorded_at timestamp for each node
func GetLastSnapshotTimes() (map[int]time.Time, error) {
	if DB == nil {
		return nil, fmt.Errorf("db not initialized")
	}

	query := fmt.Sprintf("SELECT node_id, MAX(recorded_at) FROM %shistory GROUP BY node_id", config.Current.DBPrefix)
	rows, err := DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	times := make(map[int]time.Time)
	for rows.Next() {
		var nodeID int
		var t time.Time
		if err := rows.Scan(&nodeID, &t); err != nil {
			return nil, err
		}
		times[nodeID] = t
	}
	return times, nil
}

// RecordSnapshot saves a snapshot of the node's stats into the history table
func RecordSnapshot(nodeID int, cpu float64, memUsed, memTotal, netRx, netTx, diskUsed, diskTotal, uptime int64) error {
	if DB == nil {
		return fmt.Errorf("db not initialized")
	}

	query := fmt.Sprintf(`
		INSERT INTO %shistory (node_id, cpu_usage, mem_used, mem_total, net_rx_bytes, net_tx_bytes, disk_used, disk_total, uptime)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, config.Current.DBPrefix)

	_, err := DB.Exec(query, nodeID, cpu, memUsed, memTotal, netRx, netTx, diskUsed, diskTotal, uptime)
	return err
}

// RecordSnapshotWithTime saves a snapshot with a specific timestamp
func RecordSnapshotWithTime(nodeID int, cpu float64, memUsed, memTotal, netRx, netTx, diskUsed, diskTotal, uptime int64, recordedAt time.Time) error {
	if DB == nil {
		return fmt.Errorf("db not initialized")
	}

	query := fmt.Sprintf(`
		INSERT INTO %shistory (node_id, cpu_usage, mem_used, mem_total, net_rx_bytes, net_tx_bytes, disk_used, disk_total, uptime, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, config.Current.DBPrefix)

	_, err := DB.Exec(query, nodeID, cpu, memUsed, memTotal, netRx, netTx, diskUsed, diskTotal, uptime, recordedAt)
	return err
}

// AddNode adds a new node
func AddNode(name, location, secret string, isAdminOnly bool, cfg NodeConfig) error {
	cfgBytes, _ := json.Marshal(cfg)
	query := fmt.Sprintf("INSERT INTO %snodes (name, location, secret, is_admin_only, config) VALUES (?, ?, ?, ?, ?)", config.Current.DBPrefix)
	_, err := DB.Exec(query, name, location, secret, isAdminOnly, string(cfgBytes))
	return err
}

// UpdateNode updates an existing node
func UpdateNode(id int, name, location string, isAdminOnly bool, cfg NodeConfig) error {
	cfgBytes, _ := json.Marshal(cfg)
	query := fmt.Sprintf("UPDATE %snodes SET name=?, location=?, is_admin_only=?, config=? WHERE id=?", config.Current.DBPrefix)
	_, err := DB.Exec(query, name, location, isAdminOnly, string(cfgBytes), id)
	return err
}

// UpdateNodeConfig updates only the configuration of an existing node
func UpdateNodeConfig(id int, cfg NodeConfig) error {
	cfgBytes, _ := json.Marshal(cfg)
	query := fmt.Sprintf("UPDATE %snodes SET config=? WHERE id=?", config.Current.DBPrefix)
	_, err := DB.Exec(query, string(cfgBytes), id)
	return err
}

// DeleteNode removes a node
func DeleteNode(id int) error {
	query := fmt.Sprintf("DELETE FROM %snodes WHERE id = ?", config.Current.DBPrefix)
	_, err := DB.Exec(query, id)
	return err
}

// RecordEvent logs an event for a node
func RecordEvent(nodeID int, event string) error {
	if DB == nil {
		return fmt.Errorf("db not initialized")
	}
	query := fmt.Sprintf("INSERT INTO %snode_events (node_id, event) VALUES (?, ?)", config.Current.DBPrefix)
	_, err := DB.Exec(query, nodeID, event)
	return err
}

// NodeEvent represents an event in the node_events table
type NodeEvent struct {
	Event     string    `json:"event"`
	CreatedAt time.Time `json:"created_at"`
}

// GetLatestEvents retrieves the latest events for a node
func GetLatestEvents(nodeID int, limit int) ([]NodeEvent, error) {
	if DB == nil {
		return nil, fmt.Errorf("db not initialized")
	}
	query := fmt.Sprintf("SELECT event, created_at FROM %snode_events WHERE node_id = ? ORDER BY created_at DESC LIMIT ?", config.Current.DBPrefix)
	rows, err := DB.Query(query, nodeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []NodeEvent
	for rows.Next() {
		var e NodeEvent
		if err := rows.Scan(&e.Event, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, nil
}

// HistoryPoint represents a snapshot from the history table
type HistoryPoint struct {
	RecordedAt time.Time `json:"recorded_at"`
	CPU        float64   `json:"cpu"`
	MemUsed    int64     `json:"mem_used"`
	MemTotal   int64     `json:"mem_total"`
	NetRx      int64     `json:"net_rx"`
	NetTx      int64     `json:"net_tx"`
	DiskUsed   int64     `json:"disk_used"`
	DiskTotal  int64     `json:"disk_total"`
}

// GetHistory retrieves historical data for a node since a specific time
func GetHistory(nodeID int, since time.Time) ([]HistoryPoint, error) {
	if DB == nil {
		return nil, fmt.Errorf("db not initialized")
	}
	query := fmt.Sprintf("SELECT recorded_at, cpu_usage, mem_used, mem_total, net_rx_bytes, net_tx_bytes, disk_used, disk_total FROM %shistory WHERE node_id = ? AND recorded_at >= ? ORDER BY recorded_at ASC", config.Current.DBPrefix)
	rows, err := DB.Query(query, nodeID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []HistoryPoint
	for rows.Next() {
		var h HistoryPoint
		if err := rows.Scan(&h.RecordedAt, &h.CPU, &h.MemUsed, &h.MemTotal, &h.NetRx, &h.NetTx, &h.DiskUsed, &h.DiskTotal); err != nil {
			return nil, err
		}
		history = append(history, h)
	}
	return history, nil
}
