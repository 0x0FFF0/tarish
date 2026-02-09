package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	configFileName           = "config.json"
	DefaultCheckIntervalHrs = 6
)

// Config holds persistent tarish settings
type Config struct {
	AutoUpdate         bool   `json:"auto_update"`
	CheckIntervalHours int    `json:"check_interval_hours,omitempty"` // default 6
	LastChecked        string `json:"last_checked,omitempty"`         // RFC3339
}

// configDir returns ~/.tarish (shared with install data dir)
func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".tarish"), nil
}

func configPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configFileName), nil
}

// Load reads config from disk; returns defaults on any error
func Load() *Config {
	path, err := configPath()
	if err != nil {
		return &Config{}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return &Config{}
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return &Config{}
	}
	return &cfg
}

// Save writes config to disk
func Save(cfg *Config) error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(dir, configFileName)
	return os.WriteFile(path, data, 0644)
}

// SetAutoUpdate persists the auto-update preference
func SetAutoUpdate(enabled bool) error {
	cfg := Load()
	cfg.AutoUpdate = enabled
	if enabled && cfg.CheckIntervalHours == 0 {
		cfg.CheckIntervalHours = DefaultCheckIntervalHrs
	}
	return Save(cfg)
}

// IsAutoUpdateEnabled returns the current auto-update preference
func IsAutoUpdateEnabled() bool {
	return Load().AutoUpdate
}

// checkInterval returns the effective interval duration
func (c *Config) checkInterval() time.Duration {
	hrs := c.CheckIntervalHours
	if hrs <= 0 {
		hrs = DefaultCheckIntervalHrs
	}
	return time.Duration(hrs) * time.Hour
}

// ShouldCheck returns true if auto-update is enabled and the cooldown has elapsed
func ShouldCheck() bool {
	cfg := Load()
	if !cfg.AutoUpdate {
		return false
	}
	if cfg.LastChecked == "" {
		return true
	}
	last, err := time.Parse(time.RFC3339, cfg.LastChecked)
	if err != nil {
		return true // corrupted timestamp, just check
	}
	return time.Since(last) >= cfg.checkInterval()
}

// RecordCheck stamps the current time as last checked
func RecordCheck() {
	cfg := Load()
	cfg.LastChecked = time.Now().UTC().Format(time.RFC3339)
	Save(cfg) // best-effort, ignore error
}

// FormatStatus returns a human-readable summary of the auto-update config
func FormatStatus() string {
	cfg := Load()
	if !cfg.AutoUpdate {
		return "disabled"
	}
	hrs := cfg.CheckIntervalHours
	if hrs <= 0 {
		hrs = DefaultCheckIntervalHrs
	}
	return fmt.Sprintf("enabled (every %dh)", hrs)
}
