package config

import (
	"encoding/json"
	"os"
	"sync"
)

// AppConfig represents the overall configuration loaded from file
type AppConfig struct {
	IsSetup      bool   `json:"is_setup"`
	DBHost       string `json:"db_host"`
	DBPort       string `json:"db_port"`
	DBUser       string `json:"db_user"`
	DBPassword   string `json:"db_password"`
	DBName       string `json:"db_name"`
	DBPrefix     string `json:"db_prefix"`
	AdminUser    string `json:"admin_user"`
	AdminPass    string `json:"admin_pass"` // bcrypted
	SnapshotMins         int    `json:"snapshot_mins"` // e.g. 5
	ShowDetails          bool   `json:"show_details"`
	GlobalReportInterval int    `json:"global_report_interval"`
	SiteTitle            string `json:"site_title"`
	Announcement         string `json:"announcement"`
}

var (
	cfgPath = "config.json"
	cfgLock sync.RWMutex
	Current *AppConfig
)

// Load reads the config file, creates a default one if not exists
func Load() (*AppConfig, error) {
	cfgLock.Lock()
	defer cfgLock.Unlock()

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Create default config
			Current = &AppConfig{
				IsSetup:              false,
				SnapshotMins:         5,
				GlobalReportInterval: 10,
				SiteTitle:            "FastProbe",
			}
			return Current, nil
		}
		return nil, err
	}

	Current = &AppConfig{}
	err = json.Unmarshal(data, Current)
	if Current.GlobalReportInterval <= 0 {
		Current.GlobalReportInterval = 10
	}
	if Current.SiteTitle == "" {
		Current.SiteTitle = "FastProbe"
	}
	return Current, err
}

// Save writes the current config to file
func Save() error {
	cfgLock.RLock()
	defer cfgLock.RUnlock()

	data, err := json.MarshalIndent(Current, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cfgPath, data, 0600)
}

// UpdateAndSave updates the global config and saves it
func UpdateAndSave(newCfg AppConfig) error {
	cfgLock.Lock()
	defer cfgLock.Unlock()

	Current = &newCfg
	data, err := json.MarshalIndent(Current, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cfgPath, data, 0600)
}
