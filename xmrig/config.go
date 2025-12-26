package xmrig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"tarish/cpu"
	"tarish/embedded"
)

// Config represents the xmrig configuration structure (partial)
type Config struct {
	Autosave    bool        `json:"autosave,omitempty"`
	CPU         interface{} `json:"cpu,omitempty"`
	DonateLevel int         `json:"donate-level,omitempty"`
	Pools       []Pool      `json:"pools,omitempty"`
	HTTP        *HTTPConfig `json:"http,omitempty"`
	// Store all other fields
	Raw map[string]interface{} `json:"-"`
}

// Pool represents a mining pool configuration
type Pool struct {
	URL       string `json:"url"`
	User      string `json:"user"`
	Pass      string `json:"pass,omitempty"`
	Keepalive bool   `json:"keepalive,omitempty"`
	TLS       bool   `json:"tls,omitempty"`
}

// HTTPConfig represents xmrig's HTTP API configuration
type HTTPConfig struct {
	Enabled     bool   `json:"enabled"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	AccessToken string `json:"access-token,omitempty"`
	Restricted  bool   `json:"restricted"`
}

// SelectConfig finds the most appropriate config file for the detected CPU
func SelectConfig(cpuInfo *cpu.Info, configsPath string) (string, error) {
	// List of config file candidates in priority order
	candidates := buildConfigCandidates(cpuInfo)

	for _, candidate := range candidates {
		configPath := filepath.Join(configsPath, candidate)
		if _, err := os.Stat(configPath); err == nil {
			return configPath, nil
		}
	}

	return "", fmt.Errorf("no suitable config found for CPU: %s (family: %s)", cpuInfo.RawModel, cpuInfo.Family)
}

// buildConfigCandidates returns a prioritized list of config filenames to try
func buildConfigCandidates(cpuInfo *cpu.Info) []string {
	var candidates []string

	// 1. Exact family match (e.g., apple_m3_pro.json)
	candidates = append(candidates, cpuInfo.Family+".json")

	// 2. Short form match for Apple chips (e.g., m3pro.json, m1.json)
	shortName := getShortName(cpuInfo.Family)
	if shortName != "" {
		candidates = append(candidates, shortName+".json")
	}

	// 3. Base family match (e.g., apple_m3.json for apple_m3_pro)
	baseFamily := getBaseFamily(cpuInfo.Family)
	if baseFamily != cpuInfo.Family {
		candidates = append(candidates, baseFamily+".json")
		// Also try short form of base family (e.g., m3.json)
		baseShort := getShortName(baseFamily)
		if baseShort != "" {
			candidates = append(candidates, baseShort+".json")
		}
	}

	// 4. Vendor match (e.g., apple.json, intel.json, amd.json)
	vendor := getVendor(cpuInfo.Family)
	if vendor != "" && vendor != cpuInfo.Family && vendor != baseFamily {
		candidates = append(candidates, vendor+".json")
	}

	// 5. Architecture-specific default (e.g., arm64_default.json)
	candidates = append(candidates, cpuInfo.Arch+"_default.json")

	// 6. OS-specific default (e.g., darwin_default.json)
	candidates = append(candidates, cpuInfo.OS+"_default.json")

	// 7. Generic fallback
	candidates = append(candidates, "default.json")

	return candidates
}

// getShortName converts family to short config name (e.g., apple_m3_pro -> m3pro)
func getShortName(family string) string {
	// Handle Apple Silicon short names
	if strings.HasPrefix(family, "apple_m") {
		// Remove "apple_" prefix
		name := strings.TrimPrefix(family, "apple_")
		// Remove underscores (m3_pro -> m3pro)
		name = strings.ReplaceAll(name, "_", "")
		return name
	}

	// Handle AMD Ryzen (e.g., amd_ryzen9 -> ryzen9)
	if strings.HasPrefix(family, "amd_") {
		return strings.TrimPrefix(family, "amd_")
	}

	// Handle Intel (e.g., intel_i9 -> i9)
	if strings.HasPrefix(family, "intel_") {
		return strings.TrimPrefix(family, "intel_")
	}

	return ""
}

// getBaseFamily extracts the base family (e.g., "apple_m3" from "apple_m3_pro")
func getBaseFamily(family string) string {
	// Map variant suffixes to their base
	variants := []string{"_ultra", "_max", "_pro"}
	for _, suffix := range variants {
		if strings.HasSuffix(family, suffix) {
			return strings.TrimSuffix(family, suffix)
		}
	}

	// Handle intel/amd specific variants
	if strings.HasPrefix(family, "intel_i") || strings.HasPrefix(family, "amd_ryzen") {
		return getVendor(family)
	}

	return family
}

// getVendor extracts the vendor from family (e.g., "apple" from "apple_m3_pro")
func getVendor(family string) string {
	if strings.HasPrefix(family, "apple") {
		return "apple"
	}
	if strings.HasPrefix(family, "intel") {
		return "intel"
	}
	if strings.HasPrefix(family, "amd") {
		return "amd"
	}
	return ""
}

// GetInstalledConfigPath returns the path to installed configs directory
func GetInstalledConfigPath() string {
	// Check standard installation path
	installPath := "/usr/local/share/tarish/configs"
	if _, err := os.Stat(installPath); err == nil {
		return installPath
	}

	// Fallback to relative path (for development)
	execPath, err := os.Executable()
	if err == nil {
		execDir := filepath.Dir(execPath)
		devPath := filepath.Join(execDir, "configs")
		if _, err := os.Stat(devPath); err == nil {
			return devPath
		}
	}

	// Try current working directory
	cwd, _ := os.Getwd()
	cwdPath := filepath.Join(cwd, "configs")
	if _, err := os.Stat(cwdPath); err == nil {
		return cwdPath
	}

	// Fallback: extract from embedded assets on-demand
	fmt.Println("  Extracting configs from embedded assets...")
	if err := embedded.ExtractConfigs(embedded.SharePath); err == nil {
		return installPath
	}

	return installPath // Return default even if not found
}

// LoadConfig loads and parses an xmrig config file
func LoadConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	// Parse into raw map first to preserve all fields
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Parse into struct
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	config.Raw = raw
	return &config, nil
}

// GetConfigForCurrentSystem detects CPU and returns the appropriate config path
func GetConfigForCurrentSystem() (string, *cpu.Info, error) {
	cpuInfo, err := cpu.Detect()
	if err != nil {
		return "", nil, fmt.Errorf("failed to detect CPU: %w", err)
	}

	configsPath := GetInstalledConfigPath()
	configPath, err := SelectConfig(cpuInfo, configsPath)
	if err != nil {
		return "", cpuInfo, err
	}

	return configPath, cpuInfo, nil
}

// ListAvailableConfigs returns all config files in the configs directory
func ListAvailableConfigs() ([]string, error) {
	configsPath := GetInstalledConfigPath()
	entries, err := os.ReadDir(configsPath)
	if err != nil {
		// Fallback to embedded configs list
		return embedded.ListEmbeddedConfigs()
	}

	var configs []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			configs = append(configs, entry.Name())
		}
	}

	return configs, nil
}

// GetDataDir returns the tarish data directory path
func GetDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}
	return filepath.Join(home, ".tarish")
}

// EnsureDataDir creates the data directory if it doesn't exist
func EnsureDataDir() error {
	dataDir := GetDataDir()
	return os.MkdirAll(dataDir, 0755)
}

// GetPIDFile returns the path to the PID file
func GetPIDFile() string {
	// Store PID file in the log directory which is world-writable
	return filepath.Join(GetLogDir(), "xmrig.pid")
}

// GetLogDir returns the log directory path
func GetLogDir() string {
	return "/usr/local/share/tarish/log"
}

// GetLogFile returns the path to the log file
func GetLogFile() string {
	return filepath.Join(GetLogDir(), "xmrig.log")
}

// EnsureLogDir creates the log directory if it doesn't exist
func EnsureLogDir() error {
	// Try to create with 777 permissions
	// Note: umask might still restrict this, but we try our best
	if err := os.MkdirAll(GetLogDir(), 0777); err != nil {
		return err
	}
	// Force permissions if we own the directory or have rights
	os.Chmod(GetLogDir(), 0777)
	return nil
}

// GetBinPath returns the binary search path based on OS
func GetBinPath() string {
	// Check installed location first
	installPath := "/usr/local/share/tarish/bin"
	if _, err := os.Stat(installPath); err == nil {
		return installPath
	}

	// Check relative to executable
	execPath, err := os.Executable()
	if err == nil {
		binPath := filepath.Join(filepath.Dir(execPath), "bin")
		if _, err := os.Stat(binPath); err == nil {
			return binPath
		}
	}

	// Check current directory
	cwd, _ := os.Getwd()
	cwdBin := filepath.Join(cwd, "bin")
	if _, err := os.Stat(cwdBin); err == nil {
		return cwdBin
	}

	return installPath
}

// GetPlatformName returns the platform identifier used in binary names
func GetPlatformName() string {
	osName := runtime.GOOS
	if osName == "darwin" {
		osName = "macos"
	}
	return fmt.Sprintf("%s_%s", osName, runtime.GOARCH)
}
