package db

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"vps-probe/config"

	_ "github.com/go-sql-driver/mysql"
)

var DB *sql.DB

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

	_, err = DB.Exec(historyTable)
	if err != nil {
		log.Printf("Error creating history table: %v", err)
		return err
	}

	return nil
}

// Node represents a node configuration in the database
type Node struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Location    string `json:"location"`
	Secret      string `json:"secret"`
	IsAdminOnly bool   `json:"is_admin_only"`
}

// GetAllNodes retrieves all nodes from the database
func GetAllNodes() ([]Node, error) {
	if DB == nil {
		return nil, fmt.Errorf("db not initialized")
	}

	query := fmt.Sprintf("SELECT id, name, location, secret, is_admin_only FROM %snodes", config.Current.DBPrefix)
	rows, err := DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		if err := rows.Scan(&n.ID, &n.Name, &n.Location, &n.Secret, &n.IsAdminOnly); err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
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

// AddNode adds a new node
func AddNode(name, location, secret string, isAdminOnly bool) error {
	query := fmt.Sprintf("INSERT INTO %snodes (name, location, secret, is_admin_only) VALUES (?, ?, ?, ?)", config.Current.DBPrefix)
	_, err := DB.Exec(query, name, location, secret, isAdminOnly)
	return err
}

// DeleteNode removes a node
func DeleteNode(id int) error {
	query := fmt.Sprintf("DELETE FROM %snodes WHERE id = ?", config.Current.DBPrefix)
	_, err := DB.Exec(query, id)
	return err
}
