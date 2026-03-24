package xmrig

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	"tarish/cpu"
)

func TestPrepareRuntimeConfigUsesActiveLogFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalListenPort := listenPort
	listenPort = func(host string, port int) (net.Listener, error) {
		return &fakeListener{addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: port}}, nil
	}
	t.Cleanup(func() {
		listenPort = originalListenPort
	})

	configDir := filepath.Join(home, ".local", "share", "tarish", "configs")
	logDir := filepath.Join(home, ".local", "share", "tarish", "log")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir configs: %v", err)
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatalf("mkdir log: %v", err)
	}

	configPath := filepath.Join(configDir, "test.json")
	configJSON := `{
	  "api": {"id": null, "worker-id": null},
	  "http": {"enabled": true, "port": 8181, "access-token": "Hello2025@"},
	  "log-file": "/usr/local/share/tarish/log/xmrig.log",
	  "pools": [{"url": "150.230.194.138:3333", "user": "wallet", "pass": "x"}]
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cpuInfo := &cpu.Info{Family: "apple_m1_pro"}
	runtimePath, err := PrepareRuntimeConfig(configPath, cpuInfo)
	if err != nil {
		t.Fatalf("PrepareRuntimeConfig: %v", err)
	}

	data, err := os.ReadFile(runtimePath)
	if err != nil {
		t.Fatalf("read runtime config: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal runtime config: %v", err)
	}

	logFile, _ := raw["log-file"].(string)
	if want := GetLogFile(); logFile != want {
		t.Fatalf("runtime log-file = %q, want %q", logFile, want)
	}
}

func TestPrepareRuntimeConfigChoosesAvailableHTTPPort(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configDir := filepath.Join(home, ".local", "share", "tarish", "configs")
	logDir := filepath.Join(home, ".local", "share", "tarish", "log")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir configs: %v", err)
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatalf("mkdir log: %v", err)
	}

	occupiedPort := 28181
	selectedPort := 39123
	originalListenPort := listenPort
	listenPort = func(host string, port int) (net.Listener, error) {
		switch port {
		case occupiedPort:
			return nil, errors.New("address already in use")
		case 0:
			return &fakeListener{addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: selectedPort}}, nil
		default:
			return &fakeListener{addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: port}}, nil
		}
	}
	t.Cleanup(func() {
		listenPort = originalListenPort
	})

	configPath := filepath.Join(configDir, "test.json")
	configJSON := `{
	  "api": {"id": null, "worker-id": null},
	  "http": {"enabled": true, "host": "0.0.0.0", "port": %d, "access-token": "Hello2025@"},
	  "log-file": "/usr/local/share/tarish/log/xmrig.log",
	  "pools": [{"url": "150.230.194.138:3333", "user": "wallet", "pass": "x"}]
	}`
	if err := os.WriteFile(configPath, []byte(fmt.Sprintf(configJSON, occupiedPort)), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cpuInfo := &cpu.Info{Family: "apple_m1_pro"}
	runtimePath, err := PrepareRuntimeConfig(configPath, cpuInfo)
	if err != nil {
		t.Fatalf("PrepareRuntimeConfig: %v", err)
	}

	data, err := os.ReadFile(runtimePath)
	if err != nil {
		t.Fatalf("read runtime config: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal runtime config: %v", err)
	}

	httpSection, ok := raw["http"].(map[string]interface{})
	if !ok {
		t.Fatal("missing http section")
	}

	port, ok := httpSection["port"].(float64)
	if !ok {
		t.Fatal("missing http port")
	}
	if int(port) != selectedPort {
		t.Fatalf("runtime port = %d, want %d", int(port), selectedPort)
	}
}

func TestParseLogFileUsesConfiguredRuntimeLogFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configDir := filepath.Join(home, ".local", "share", "tarish", "configs")
	logDir := filepath.Join(home, ".local", "share", "tarish", "log")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir configs: %v", err)
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatalf("mkdir log: %v", err)
	}

	configuredLog := filepath.Join(t.TempDir(), "configured-xmrig.log")
	runtimeJSON := `{"log-file":"` + configuredLog + `"}`
	if err := os.WriteFile(GetRuntimeConfigPath(), []byte(runtimeJSON), 0644); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	logContent := "speed 10s/60s/15m 123.45 120.00 n/a H/s max 140.00 H/s\n"
	if err := os.WriteFile(configuredLog, []byte(logContent), 0644); err != nil {
		t.Fatalf("write configured log: %v", err)
	}

	status, err := parseLogFile()
	if err != nil {
		t.Fatalf("parseLogFile: %v", err)
	}
	if status.Hashrate == nil {
		t.Fatal("expected hashrate from configured runtime log file")
	}
	if status.Hashrate.Current != 123.45 || status.Hashrate.Average != 120.00 || status.Hashrate.Max != 140.00 {
		t.Fatalf("unexpected hashrate: %+v", status.Hashrate)
	}
}

func TestParseLogFileFallsBackToTarishLogFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configDir := filepath.Join(home, ".local", "share", "tarish", "configs")
	logDir := filepath.Join(home, ".local", "share", "tarish", "log")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("mkdir configs: %v", err)
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatalf("mkdir log: %v", err)
	}

	configuredLog := filepath.Join(t.TempDir(), "configured-xmrig.log")
	runtimeJSON := `{"log-file":"` + configuredLog + `"}`
	if err := os.WriteFile(GetRuntimeConfigPath(), []byte(runtimeJSON), 0644); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}
	if err := os.WriteFile(configuredLog, []byte(""), 0644); err != nil {
		t.Fatalf("write configured log: %v", err)
	}

	fallbackLog := GetLogFile()
	logContent := "speed 10s/60s/15m 456.78 430.00 n/a H/s max 520.00 H/s\n"
	if err := os.WriteFile(fallbackLog, []byte(logContent), 0644); err != nil {
		t.Fatalf("write fallback log: %v", err)
	}

	status, err := parseLogFile()
	if err != nil {
		t.Fatalf("parseLogFile: %v", err)
	}
	if status.Hashrate == nil {
		t.Fatal("expected hashrate from tarish fallback log file")
	}
	if status.Hashrate.Current != 456.78 || status.Hashrate.Average != 430.00 || status.Hashrate.Max != 520.00 {
		t.Fatalf("unexpected hashrate: %+v", status.Hashrate)
	}
}

type fakeListener struct {
	addr net.Addr
}

func (l *fakeListener) Accept() (net.Conn, error) {
	return nil, errors.New("not implemented")
}

func (l *fakeListener) Close() error {
	return nil
}

func (l *fakeListener) Addr() net.Addr {
	return l.addr
}
