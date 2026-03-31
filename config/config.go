package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tarish/userctx"
)

const (
	configFileName          = "tarish.json"
	DefaultCheckIntervalHrs = 2
)

// Config holds persistent tarish settings
type Config struct {
	AutoUpdate               bool   `json:"auto_update"`
	CheckIntervalHours       int    `json:"check_interval_hours,omitempty"` // default 6
	LastChecked              string `json:"last_checked,omitempty"`         // RFC3339
	TLSXmrigProxy            *bool  `json:"tls-xmrig-proxy,omitempty"`      // default true
	ServerURL                string `json:"server_url,omitempty"`
	ServerEnabled            *bool  `json:"server_enabled,omitempty"`
	ServerAgentKey           string `json:"server_agent_key,omitempty"`
	ServerAccessClientID     string `json:"server_access_client_id,omitempty"`
	ServerAccessClientSecret string `json:"server_access_client_secret,omitempty"`
	ServerAPIKey             string `json:"server_api_key,omitempty"` // deprecated, migrated to server_agent_key
}

// ConfigDir returns the tarish config directory for the owning user account.
func ConfigDir() (string, error) {
	home, err := userctx.HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "tarish"), nil
}

func configPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configFileName), nil
}

// legacyConfigPath returns the old location for one-time migration
func legacyConfigPath() (string, error) {
	home, err := userctx.HomeDir()
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
	// Migrate deprecated server_api_key -> server_agent_key
	if cfg.ServerAgentKey == "" && cfg.ServerAPIKey != "" {
		cfg.ServerAgentKey = cfg.ServerAPIKey
		cfg.ServerAPIKey = ""
		_ = Save(&cfg)
	}
	return &cfg
}

// Save writes config to disk
func Save(cfg *Config) error {
	dir, err := ConfigDir()
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

// GetCheckInterval returns the configured auto-update check interval.
func GetCheckInterval() time.Duration {
	return Load().checkInterval()
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

func serverEnabledDefault(cfg *Config) bool {
	return strings.TrimSpace(cfg.ServerURL) != ""
}

// GetServerURL returns the configured tarish server URL (empty if not set)
func GetServerURL() string {
	return Load().ServerURL
}

// SetServerURL persists the tarish server URL
func SetServerURL(url string) error {
	cfg := Load()
	cfg.ServerURL = url
	return Save(cfg)
}

// IsServerEnabled returns whether miner-to-server communication is enabled.
// Older configs implicitly enable communication when a server URL is present.
func IsServerEnabled() bool {
	cfg := Load()
	if cfg.ServerEnabled == nil {
		return serverEnabledDefault(cfg)
	}
	return *cfg.ServerEnabled
}

// SetServerEnabled persists the miner-to-server communication preference.
func SetServerEnabled(enabled bool) error {
	cfg := Load()
	cfg.ServerEnabled = &enabled
	return Save(cfg)
}

// FormatServerStatus returns a human-readable summary of server communication.
func FormatServerStatus() string {
	if IsServerEnabled() {
		return "enabled"
	}
	return "disabled"
}

// GetServerAgentKey returns the configured agent key for server auth
func GetServerAgentKey() string {
	return Load().ServerAgentKey
}

// SetServerAgentKey persists the agent key for server authentication
func SetServerAgentKey(key string) error {
	cfg := Load()
	cfg.ServerAgentKey = key
	return Save(cfg)
}

// GetServerAccessClientID returns the configured Cloudflare Access client ID.
func GetServerAccessClientID() string {
	return Load().ServerAccessClientID
}

// SetServerAccessClientID persists the Cloudflare Access client ID.
func SetServerAccessClientID(id string) error {
	cfg := Load()
	cfg.ServerAccessClientID = id
	return Save(cfg)
}

// GetServerAccessClientSecret returns the configured Cloudflare Access client secret.
func GetServerAccessClientSecret() string {
	return Load().ServerAccessClientSecret
}

// SetServerAccessClientSecret persists the Cloudflare Access client secret.
func SetServerAccessClientSecret(secret string) error {
	cfg := Load()
	cfg.ServerAccessClientSecret = secret
	return Save(cfg)
}

// GetServerAPIKey is deprecated, use GetServerAgentKey
func GetServerAPIKey() string { return GetServerAgentKey() }

// SetServerAPIKey is deprecated, use SetServerAgentKey
func SetServerAPIKey(key string) error { return SetServerAgentKey(key) }
