package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"tarish/config"
	"tarish/cpu"
	"tarish/xmrig"
)

// Version is set from main.go at startup
var Version = "dev"

const (
	heartbeatInterval  = 30 * time.Second
	configPollInterval = 3 * time.Second
	httpTimeout        = 10 * time.Second
)

// Guards applyConfigOverride so the heartbeat and config-poll don't race.
var configMu sync.Mutex

type ReportResponse struct {
	OK             bool                   `json:"ok"`
	ConfigOverride map[string]interface{} `json:"config_override,omitempty"`
}

// RunDaemon runs the agent heartbeat loop. Blocks until killed.
// Invoked via the hidden "_agent-daemon" command.
func RunDaemon() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)

	if !config.IsServerEnabled() {
		fmt.Println("[agent] server communication disabled, exiting")
		return
	}

	serverURL := config.GetServerURL()
	if serverURL == "" {
		fmt.Println("[agent] no server URL configured, exiting")
		return
	}

	cpuInfo, err := cpu.Detect()
	if err != nil {
		fmt.Printf("[agent] failed to detect CPU: %v\n", err)
		return
	}

	fmt.Printf("[agent] started (pid %d), reporting to %s every %v\n",
		os.Getpid(), serverURL, heartbeatInterval)
	fmt.Printf("[agent] CPU: %s (%s, %d cores)\n", cpuInfo.RawModel, cpuInfo.Family, cpuInfo.Cores)

	// Initial delay to let xmrig fully start
	select {
	case <-time.After(5 * time.Second):
	case <-sig:
		fmt.Println("[agent] received signal during startup, exiting")
		return
	}

	sendReport(cpuInfo, serverURL)

	// Fast config-poll loop: checks for pending overrides every 3s so
	// dashboard config edits are applied almost immediately.
	stopPoll := make(chan struct{})
	go pollConfigLoop(stopPoll)

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !config.IsServerEnabled() {
				fmt.Println("[agent] server communication disabled, exiting")
				close(stopPoll)
				return
			}
			currentServerURL := config.GetServerURL()
			if currentServerURL == "" {
				fmt.Println("[agent] server URL removed, exiting")
				close(stopPoll)
				return
			}
			sendReport(cpuInfo, currentServerURL)
		case <-sig:
			fmt.Println("[agent] received signal, shutting down")
			close(stopPoll)
			return
		}
	}
}

// StartDaemon spawns the agent daemon as a background process.
func StartDaemon() error {
	if !config.IsServerEnabled() {
		fmt.Println("Agent: server communication disabled, skipping (use 'tarish server enable')")
		return nil
	}

	serverURL := config.GetServerURL()
	if serverURL == "" {
		fmt.Println("Agent: no server URL configured, skipping (use 'tarish server set <url>')")
		return nil
	}

	if _, running := IsDaemonRunning(); running {
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot locate tarish binary: %w", err)
	}
	exe, _ = filepath.EvalSymlinks(exe)

	logDir := daemonLogDir()
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("cannot create log dir: %w", err)
	}

	logPath := filepath.Join(logDir, "agent-daemon.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("cannot open daemon log: %w", err)
	}

	cmd := exec.Command(exe, "_agent-daemon")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("failed to start agent daemon: %w", err)
	}

	if err := saveDaemonPID(cmd.Process.Pid); err != nil {
		cmd.Process.Kill()
		logFile.Close()
		return err
	}

	go func() {
		cmd.Wait()
		logFile.Close()
		os.Remove(daemonPIDFile())
	}()

	fmt.Printf("Agent: reporting to %s (pid %d)\n", serverURL, cmd.Process.Pid)
	return nil
}

// StopDaemon sends SIGTERM to the agent daemon (if running).
func StopDaemon() {
	pid, running := IsDaemonRunning()
	if !running {
		return
	}
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Signal(syscall.SIGTERM)
		time.Sleep(200 * time.Millisecond)
		if isProcessAlive(pid) {
			_ = p.Signal(syscall.SIGKILL)
		}
	}
	os.Remove(daemonPIDFile())
}

// IsDaemonRunning reports the PID and whether the agent daemon is alive.
func IsDaemonRunning() (int, bool) {
	data, err := os.ReadFile(daemonPIDFile())
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false
	}
	return pid, isProcessAlive(pid)
}

func sendReport(cpuInfo *cpu.Info, serverURL string) {
	report := buildReport(cpuInfo, Version)

	body, err := json.Marshal(report)
	if err != nil {
		fmt.Printf("[agent] marshal error: %v\n", err)
		return
	}

	client := &http.Client{Timeout: httpTimeout}
	url := serverURL + "/api/report"

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		fmt.Printf("[agent] request error: %v\n", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	applyServerAuthHeaders(req)

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[agent] report failed: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		fmt.Printf("[agent] server returned %d: %s\n", resp.StatusCode, string(respBody))
		return
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}

	var response ReportResponse
	if json.Unmarshal(respBody, &response) != nil {
		return
	}

	if report.Hashrate != nil {
		fmt.Printf("[agent] report ok (hashrate: %.1f H/s)\n", report.Hashrate.Current)
	} else {
		fmt.Println("[agent] report ok (hashrate: unavailable)")
	}

	if response.ConfigOverride != nil {
		agentID := preferredAgentID(report.MinerID, report.WorkerID, report.IP, report.Hostname)
		if agentID != "" {
			applyConfigOverride(response.ConfigOverride, serverURL, agentID)
		}
	}
}

// readAgentID reads the stable agent identity used by tarish-server.
func readAgentID() string {
	minerID, workerID := readRuntimeIDs()
	hostname, _ := os.Hostname()
	return preferredAgentID(minerID, workerID, detectLANIP(), hostname)
}

// pollConfigLoop polls the server for pending config overrides every few
// seconds so that dashboard edits are applied almost immediately instead
// of waiting for the next 30s heartbeat.
func pollConfigLoop(stop <-chan struct{}) {
	agentID := readAgentID()
	if agentID == "" {
		fmt.Println("[agent] config-poll: cannot determine miner ID, skipping")
		return
	}

	client := &http.Client{Timeout: 5 * time.Second}

	ticker := time.NewTicker(configPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if !config.IsServerEnabled() {
				return
			}
			serverURL := config.GetServerURL()
			if serverURL == "" {
				return
			}
			pendingURL := fmt.Sprintf("%s/api/miners/%s/config/pending", serverURL, agentID)
			checkPendingConfig(client, pendingURL, serverURL, agentID)
		}
	}
}

func checkPendingConfig(client *http.Client, pendingURL, serverURL, agentID string) {
	req, err := http.NewRequest("GET", pendingURL, nil)
	if err != nil {
		return
	}
	applyServerAuthHeaders(req)

	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}

	var response ReportResponse
	if json.Unmarshal(body, &response) != nil {
		return
	}

	if response.ConfigOverride != nil {
		applyConfigOverride(response.ConfigOverride, serverURL, agentID)
	}
}

func applyConfigOverride(override map[string]interface{}, serverURL, agentID string) {
	configMu.Lock()
	defer configMu.Unlock()

	port, accessToken := xmrig.GetHTTPConfigFromRuntime()

	body, err := json.Marshal(override)
	if err != nil {
		fmt.Printf("[agent] failed to marshal config override: %v\n", err)
		return
	}

	client := &http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/1/config", port)

	req, err := http.NewRequest("PUT", url, bytes.NewReader(body))
	if err != nil {
		fmt.Printf("[agent] failed to create PUT request: %v\n", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[agent] failed to apply config: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 || resp.StatusCode == 204 {
		fmt.Println("[agent] applied config override from server")
		ackConfigOverride(serverURL, agentID)
	} else {
		respBody, _ := io.ReadAll(resp.Body)
		fmt.Printf("[agent] xmrig rejected config (HTTP %d): %s\n", resp.StatusCode, string(respBody))
	}
}

func ackConfigOverride(serverURL, agentID string) {
	client := &http.Client{Timeout: 5 * time.Second}
	ackURL := fmt.Sprintf("%s/api/miners/%s/config/ack", serverURL, agentID)

	req, err := http.NewRequest("POST", ackURL, nil)
	if err != nil {
		fmt.Printf("[agent] failed to create ack request: %v\n", err)
		return
	}

	applyServerAuthHeaders(req)

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[agent] failed to ack config: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		fmt.Println("[agent] config override acknowledged")
	} else {
		respBody, _ := io.ReadAll(resp.Body)
		fmt.Printf("[agent] ack failed (HTTP %d): %s\n", resp.StatusCode, string(respBody))
	}
}

func applyServerAuthHeaders(req *http.Request) {
	if agentKey := config.GetServerAgentKey(); agentKey != "" {
		req.Header.Set("Authorization", "Bearer "+agentKey)
	}
	if clientID := config.GetServerAccessClientID(); clientID != "" {
		req.Header.Set("CF-Access-Client-Id", clientID)
	}
	if clientSecret := config.GetServerAccessClientSecret(); clientSecret != "" {
		req.Header.Set("CF-Access-Client-Secret", clientSecret)
	}
}

// ---------- internal helpers ----------

func daemonPIDFile() string {
	dir, err := config.ConfigDir()
	if err != nil {
		return "/tmp/tarish-agent-daemon.pid"
	}
	return filepath.Join(dir, "agent-daemon.pid")
}

func daemonLogDir() string {
	dir, err := config.ConfigDir()
	if err != nil {
		return "/tmp"
	}
	return filepath.Join(dir, "log")
}

func saveDaemonPID(pid int) error {
	path := daemonPIDFile()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.Itoa(pid)), 0644)
}

func isProcessAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
