package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	configFileName          = "tarish.json"
	DefaultCheckIntervalHrs = 6
)

// Config holds persistent tarish settings
type Config struct {
	AutoUpdate         bool   `json:"auto_update"`
	CheckIntervalHours int    `json:"check_interval_hours,omitempty"` // default 6
	LastChecked        string `json:"last_checked,omitempty"`         // RFC3339
	TLSXmrigProxy      *bool  `json:"tls-xmrig-proxy,omitempty"`     // default true
}

// configDir returns ~/.local/share/tarish (user-wide, same as install share on Linux/macOS)
func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "tarish"), nil
}

func configPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configFileName), nil
}

// legacyConfigPath returns the old location for one-time migration
func legacyConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".tarish", "config.json"), nil
}

// Load reads config from disk; returns defaults on any error.
// If the new path does not exist, migrates from ~/.tarish/config.json once.
func Load() *Config {
	path, err := configPath()
	if err != nil {
		return &Config{}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		// One-time migration from legacy path
		if legacyPath, lerr := legacyConfigPath(); lerr == nil {
			if legacyData, lread := os.ReadFile(legacyPath); lread == nil {
				var cfg Config
				if json.Unmarshal(legacyData, &cfg) == nil {
					_ = Save(&cfg) // write to new path
					_ = os.Remove(legacyPath)
					return &cfg
				}
			}
		}
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

// IsTLSXmrigProxyEnabled returns whether TLS to xmrig-proxy is enabled.
// Defaults to true when the setting is absent from the config.
func IsTLSXmrigProxyEnabled() bool {
	cfg := Load()
	if cfg.TLSXmrigProxy == nil {
		return true // enabled by default
	}
	return *cfg.TLSXmrigProxy
}

// SetTLSXmrigProxy persists the TLS xmrig-proxy preference
func SetTLSXmrigProxy(enabled bool) error {
	cfg := Load()
	cfg.TLSXmrigProxy = &enabled
	return Save(cfg)
}

// FormatTLSStatus returns a human-readable summary of the TLS xmrig-proxy config
func FormatTLSStatus() string {
	if IsTLSXmrigProxyEnabled() {
		return "enabled"
	}
	return "disabled"
}
