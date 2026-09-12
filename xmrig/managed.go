package xmrig

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"tarish/config"
	"tarish/cpu"
)

type ManagedOptions struct {
	ProxyEnabled bool
	SOCKSAddr    string
	RuntimeDir   string
	TokenPath    string
}

func PrepareRuntimeConfig(configPath string, cpuInfo *cpu.Info) (string, error) {
	return PrepareManagedRuntime(configPath, cpuInfo, ManagedOptions{})
}

func PrepareManagedRuntime(configPath string, cpuInfo *cpu.Info, opts ManagedOptions) (string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("failed to read config: %w", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", fmt.Errorf("failed to parse config: %w", err)
	}

	shortName := getShortCPUName(cpuInfo.Family)
	apiID := shortName + "-0"
	workerID := buildWorkerID()
	apiSection, ok := raw["api"].(map[string]interface{})
	if !ok {
		apiSection = make(map[string]interface{})
	}
	apiSection["id"] = apiID
	apiSection["worker-id"] = workerID
	raw["api"] = apiSection

	raw["donate-level"] = 0
	raw["donate-over-proxy"] = 0
	raw["autosave"] = false

	if opts.ProxyEnabled {
		if err := applyProxyPools(raw, opts.SOCKSAddr); err != nil {
			return "", err
		}
	} else {
		applyTLSPoolSettings(raw)
	}

	token, err := loadOrCreateHTTPToken(opts.TokenPath)
	if err != nil {
		return "", err
	}
	if err := normalizeManagedHTTP(raw, token); err != nil {
		return "", err
	}

	raw["log-file"] = GetLogFile()

	runtimePath := runtimePathFor(opts)
	if err := os.MkdirAll(filepath.Dir(runtimePath), 0o700); err != nil {
		return "", fmt.Errorf("failed to create runtime dir: %w", err)
	}
	output, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal config: %w", err)
	}
	output = append(output, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(runtimePath), ".tmp-runtime-*")
	if err != nil {
		return "", fmt.Errorf("failed to write runtime config: %w", err)
	}
	tmpName := tmp.Name()
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", err
	}
	if _, err := tmp.Write(output); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	if err := os.Rename(tmpName, runtimePath); err != nil {
		os.Remove(tmpName)
		return "", fmt.Errorf("failed to write runtime config: %w", err)
	}
	return runtimePath, nil
}

func runtimePathFor(opts ManagedOptions) string {
	if opts.RuntimeDir != "" {
		return filepath.Join(opts.RuntimeDir, "xmrig_runtime.json")
	}
	return GetRuntimeConfigPath()
}

func GetRuntimeConfigPath() string {
	dir, err := config.PrivateDir()
	if err != nil {
		return filepath.Join(GetLogDir(), "xmrig_runtime.json")
	}
	return filepath.Join(dir, "runtime", "xmrig_runtime.json")
}

func applyProxyPools(raw map[string]interface{}, socksAddr string) error {
	if strings.TrimSpace(socksAddr) == "" {
		return fmt.Errorf("proxy socks address missing")
	}
	poolsRaw, ok := raw["pools"].([]interface{})
	if !ok || len(poolsRaw) == 0 {
		return fmt.Errorf("no pools in config")
	}
	first, ok := poolsRaw[0].(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid pool entry")
	}
	first["url"] = TLSPoolURL
	first["tls"] = true
	first["tls-fingerprint"] = TLSFingerprint
	first["keepalive"] = true
	first["socks5"] = socksAddr
	first["enabled"] = true
	raw["pools"] = []interface{}{first}
	return nil
}

func normalizeManagedHTTP(raw map[string]interface{}, token string) error {
	httpSection, ok := raw["http"].(map[string]interface{})
	if !ok {
		httpSection = make(map[string]interface{})
		raw["http"] = httpSection
	}
	httpSection["enabled"] = true
	httpSection["host"] = "127.0.0.1"
	httpSection["access-token"] = token
	httpSection["restricted"] = false

	port := defaultHTTPPort
	if p, ok := httpSection["port"].(float64); ok && int(p) > 0 {
		port = int(p)
	}
	selected, err := chooseAvailablePort("127.0.0.1", port)
	if err != nil {
		return fmt.Errorf("failed to reserve xmrig HTTP API port: %w", err)
	}
	httpSection["port"] = selected
	return nil
}

func loadOrCreateHTTPToken(path string) (string, error) {
	if path == "" {
		dir, err := config.PrivateDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(dir, "http_token")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if data, err := os.ReadFile(path); err == nil {
		tok := strings.TrimSpace(string(data))
		if tok != "" {
			return tok, nil
		}
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(b)
	if err := os.WriteFile(path, []byte(tok+"\n"), 0o600); err != nil {
		return "", err
	}
	return tok, nil
}

var protectedTop = map[string]bool{
	"pools": true, "http": true, "donate-level": true, "donate-over-proxy": true,
}

func SanitizeOverride(override, current map[string]interface{}, proxyEnabled bool) (map[string]interface{}, []string) {
	if override == nil {
		return map[string]interface{}{}, nil
	}
	rejected := []string{}
	out := map[string]interface{}{}
	for k, v := range override {
		if protectedTop[k] {
			rejected = append(rejected, k)
			continue
		}
		if k == "tls" {
			rejected = append(rejected, k)
			continue
		}
		out[k] = v
	}
	if current != nil {
		for k, v := range current {
			if protectedTop[k] || k == "tls" {
				out[k] = v
			}
		}
		if httpCur, ok := current["http"].(map[string]interface{}); ok {
			out["http"] = httpCur
		}
		if pools, ok := current["pools"]; ok {
			out["pools"] = pools
		}
		out["donate-level"] = 0
		out["donate-over-proxy"] = 0
	}
	_ = proxyEnabled
	return out, rejected
}

func RedactLiveConfig(cfg map[string]interface{}) map[string]interface{} {
	if cfg == nil {
		return nil
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return map[string]interface{}{}
	}
	var out map[string]interface{}
	if json.Unmarshal(raw, &out) != nil {
		return map[string]interface{}{}
	}
	if httpSection, ok := out["http"].(map[string]interface{}); ok {
		delete(httpSection, "access-token")
		httpSection["access-token"] = "[redacted]"
	}
	if pools, ok := out["pools"].([]interface{}); ok {
		for _, p := range pools {
			m, ok := p.(map[string]interface{})
			if !ok {
				continue
			}
			if socks, ok := m["socks5"].(string); ok && socks != "" {
				m["socks5"] = "127.0.0.1:[redacted]"
			}
		}
	}
	return out
}

func OverrideEnablesDonation(override map[string]interface{}) bool {
	if override == nil {
		return false
	}
	if v, ok := override["donate-level"]; ok {
		switch t := v.(type) {
		case float64:
			return t > 0
		case int:
			return t > 0
		case string:
			n, _ := strconv.Atoi(t)
			return n > 0
		}
	}
	return false
}

func StartOwned(binaryPath, configPath string) (int, error) {
	if err := EnsureDataDir(); err != nil {
		return 0, fmt.Errorf("failed to create data directory: %w", err)
	}
	if err := EnsureExecutable(binaryPath); err != nil {
		return 0, fmt.Errorf("failed to set executable permission: %w", err)
	}
	if err := EnsureLogDir(); err != nil {
		return 0, fmt.Errorf("failed to create log directory: %w", err)
	}
	logFile := GetLogFile()
	logHandle, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, fmt.Errorf("failed to create log file: %w", err)
	}

	cmd := exec.Command(binaryPath, "-c", configPath)
	cmd.Stdout = logHandle
	cmd.Stderr = logHandle
	cmd.Dir = filepath.Dir(binaryPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		logHandle.Close()
		return 0, fmt.Errorf("failed to start xmrig: %w", err)
	}
	pid := cmd.Process.Pid
	if err := savePID(pid); err != nil {
		cmd.Process.Kill()
		logHandle.Close()
		return 0, fmt.Errorf("failed to save PID: %w", err)
	}
	go func() {
		cmd.Wait()
		logHandle.Close()
		os.Remove(GetPIDFile())
	}()
	return pid, nil
}

func StopOwned(pid int) error {
	if pid <= 0 {
		return nil
	}
	_ = killProcess(pid)
	os.Remove(GetPIDFile())
	return nil
}

func StopOwnedFromPIDFile() error {
	pid, running := IsRunning()
	if running {
		return StopOwned(pid)
	}
	os.Remove(GetPIDFile())
	return nil
}

func TLSPin() string { return TLSFingerprint }

func LoopbackHost(host string) bool {
	ip := net.ParseIP(host)
	return host == "127.0.0.1" || host == "localhost" || (ip != nil && ip.IsLoopback())
}
