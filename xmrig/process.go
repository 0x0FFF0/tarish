package xmrig

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ProcessStatus represents the current state of xmrig
type ProcessStatus struct {
	Running     bool
	PID         int
	Version     string
	Uptime      time.Duration
	Hashrate    *HashrateInfo
	Pool        *PoolInfo
	DonateLevel int
}

// HashrateInfo contains hashrate statistics
type HashrateInfo struct {
	Current float64 // H/s in last 10s
	Average float64 // H/s in last 60s
	Max     float64 // Max recorded
}

// PoolInfo contains pool connection info
type PoolInfo struct {
	URL    string
	User   string
	Active bool
}

// APIResponse represents xmrig's HTTP API summary response
type APIResponse struct {
	ID       string `json:"id"`
	Version  string `json:"version"`
	Uptime   int64  `json:"uptime"`
	Hashrate struct {
		Total []float64 `json:"total"`
	} `json:"hashrate"`
	Connection struct {
		Pool     string `json:"pool"`
		User     string `json:"user"`
		Accepted int    `json:"accepted"`
		Rejected int    `json:"rejected"`
	} `json:"connection"`
}

// Start starts xmrig as a daemon process
func Start(binaryPath, configPath string, force bool) error {
	if err := EnsureDataDir(); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Check if already running
	if pid, running := IsRunning(); running {
		if !force {
			return fmt.Errorf("xmrig is already running (PID: %d). Use --force to kill and restart", pid)
		}
		fmt.Printf("Killing existing xmrig process (PID: %d)...\n", pid)
		if err := Stop(); err != nil {
			return fmt.Errorf("failed to stop existing process: %w", err)
		}
		time.Sleep(500 * time.Millisecond) // Wait for cleanup
	}

	// Ensure binary is executable
	if err := EnsureExecutable(binaryPath); err != nil {
		return fmt.Errorf("failed to set executable permission: %w", err)
	}

	// Prepare log file
	logFile := GetLogFile()
	logHandle, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}

	// Build command
	cmd := exec.Command(binaryPath, "-c", configPath)
	cmd.Stdout = logHandle
	cmd.Stderr = logHandle
	cmd.Dir = filepath.Dir(binaryPath)

	// Set process group for proper cleanup
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		logHandle.Close()
		return fmt.Errorf("failed to start xmrig: %w", err)
	}

	// Save PID
	pid := cmd.Process.Pid
	if err := savePID(pid); err != nil {
		// Try to kill the process if we can't save PID
		cmd.Process.Kill()
		logHandle.Close()
		return fmt.Errorf("failed to save PID: %w", err)
	}

	// Detach from the process (don't wait for it)
	go func() {
		cmd.Wait()
		logHandle.Close()
		// Clean up PID file if process exits
		os.Remove(GetPIDFile())
	}()

	fmt.Printf("xmrig started successfully (PID: %d)\n", pid)
	fmt.Printf("Log file: %s\n", logFile)

	return nil
}

// Stop stops all xmrig processes
func Stop() error {
	killed := false

	// First try to kill by PID file
	if pid, running := IsRunning(); running {
		if err := killProcess(pid); err == nil {
			killed = true
		}
	}

	// Clean up any orphaned xmrig processes
	orphans := findXmrigProcesses()
	for _, pid := range orphans {
		if err := killProcess(pid); err == nil {
			killed = true
		}
	}

	// Remove PID file
	os.Remove(GetPIDFile())

	if killed {
		fmt.Println("xmrig stopped successfully")
	} else {
		fmt.Println("No xmrig processes were running")
	}

	return nil
}

// IsRunning checks if xmrig is currently running
func IsRunning() (int, bool) {
	pid, err := readPID()
	if err != nil {
		return 0, false
	}

	if isProcessRunning(pid) {
		return pid, true
	}

	return 0, false
}

// Status returns the current status of xmrig
func Status() (*ProcessStatus, error) {
	status := &ProcessStatus{}

	pid, running := IsRunning()
	status.Running = running
	status.PID = pid

	if !running {
		return status, nil
	}

	// Try to get info from HTTP API first (if enabled in config)
	apiStatus, err := getAPIStatus()
	if err == nil {
		status.Version = apiStatus.Version
		status.Uptime = time.Duration(apiStatus.Uptime) * time.Second
		if len(apiStatus.Hashrate.Total) >= 3 {
			status.Hashrate = &HashrateInfo{
				Current: apiStatus.Hashrate.Total[0],
				Average: apiStatus.Hashrate.Total[1],
				Max:     apiStatus.Hashrate.Total[2],
			}
		}
		status.Pool = &PoolInfo{
			URL:    apiStatus.Connection.Pool,
			User:   apiStatus.Connection.User,
			Active: apiStatus.Connection.Accepted > 0,
		}
		return status, nil
	}

	// Fallback: parse log file for status
	logStatus, err := parseLogFile()
	if err == nil {
		if logStatus.Version != "" {
			status.Version = logStatus.Version
		}
		if logStatus.Hashrate != nil {
			status.Hashrate = logStatus.Hashrate
		}
		if logStatus.Pool != nil {
			status.Pool = logStatus.Pool
		}
		status.DonateLevel = logStatus.DonateLevel
	}

	return status, nil
}

// savePID saves the process ID to the PID file
func savePID(pid int) error {
	return os.WriteFile(GetPIDFile(), []byte(strconv.Itoa(pid)), 0644)
}

// readPID reads the process ID from the PID file
func readPID() (int, error) {
	data, err := os.ReadFile(GetPIDFile())
	if err != nil {
		return 0, err
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, err
	}

	return pid, nil
}

// isProcessRunning checks if a process with the given PID is running
func isProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// On Unix, FindProcess always succeeds, so we need to send signal 0
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// killProcess kills a process by PID
func killProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}

	// Send SIGKILL for hard kill
	if err := process.Signal(syscall.SIGKILL); err != nil {
		return err
	}

	// Wait a moment for the process to terminate
	time.Sleep(100 * time.Millisecond)

	return nil
}

// findXmrigProcesses finds all running xmrig processes
func findXmrigProcesses() []int {
	var pids []int

	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("pgrep", "-f", "xmrig")
	} else {
		cmd = exec.Command("pgrep", "xmrig")
	}

	output, err := cmd.Output()
	if err != nil {
		return pids
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if pid, err := strconv.Atoi(strings.TrimSpace(line)); err == nil {
			pids = append(pids, pid)
		}
	}

	return pids
}

// getAPIStatus tries to get status from xmrig's HTTP API
func getAPIStatus() (*APIResponse, error) {
	// Try common API ports
	ports := []int{8080, 8000, 3000}

	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	for _, port := range ports {
		url := fmt.Sprintf("http://127.0.0.1:%d/1/summary", port)
		resp, err := client.Get(url)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			continue
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			continue
		}

		var apiResp APIResponse
		if err := json.Unmarshal(body, &apiResp); err != nil {
			continue
		}

		return &apiResp, nil
	}

	return nil, fmt.Errorf("API not available")
}

// parseLogFile extracts status information from the xmrig log file
func parseLogFile() (*ProcessStatus, error) {
	logFile := GetLogFile()
	file, err := os.Open(logFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	status := &ProcessStatus{}

	// Read last 100 lines
	lines, err := tailFile(file, 100)
	if err != nil {
		return nil, err
	}

	// Regex patterns
	versionRe := regexp.MustCompile(`XMRig\s+(\d+\.\d+\.\d+)`)
	hashrateRe := regexp.MustCompile(`speed\s+[\d.]+s/[\d.]+s/[\d.]+s\s+([\d.]+)\s+([\d.]+)\s+([\d.]+)`)
	poolRe := regexp.MustCompile(`\[([^\]]+)\]\s+use\s+pool\s+(\S+)`)
	donateRe := regexp.MustCompile(`donate\s+level:\s+(\d+)%`)
	userRe := regexp.MustCompile(`\[([^\]]+)\]\s+login\s+(\S+)`)

	for _, line := range lines {
		if matches := versionRe.FindStringSubmatch(line); len(matches) > 1 {
			status.Version = matches[1]
		}

		if matches := hashrateRe.FindStringSubmatch(line); len(matches) > 3 {
			current, _ := strconv.ParseFloat(matches[1], 64)
			avg, _ := strconv.ParseFloat(matches[2], 64)
			max, _ := strconv.ParseFloat(matches[3], 64)
			status.Hashrate = &HashrateInfo{
				Current: current,
				Average: avg,
				Max:     max,
			}
		}

		if matches := poolRe.FindStringSubmatch(line); len(matches) > 2 {
			if status.Pool == nil {
				status.Pool = &PoolInfo{}
			}
			status.Pool.URL = matches[2]
			status.Pool.Active = true
		}

		if matches := userRe.FindStringSubmatch(line); len(matches) > 2 {
			if status.Pool == nil {
				status.Pool = &PoolInfo{}
			}
			status.Pool.User = matches[2]
		}

		if matches := donateRe.FindStringSubmatch(line); len(matches) > 1 {
			status.DonateLevel, _ = strconv.Atoi(matches[1])
		}
	}

	return status, nil
}

// tailFile reads the last n lines from a file
func tailFile(file *os.File, n int) ([]string, error) {
	var lines []string
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > n {
			lines = lines[1:]
		}
	}

	return lines, scanner.Err()
}

// FormatStatus formats the status for display
func (s *ProcessStatus) FormatStatus() string {
	var sb strings.Builder

	if !s.Running {
		sb.WriteString("Status: NOT RUNNING\n")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("Status: RUNNING (PID: %d)\n", s.PID))

	if s.Version != "" {
		sb.WriteString(fmt.Sprintf("Version: %s\n", s.Version))
	}

	if s.Uptime > 0 {
		sb.WriteString(fmt.Sprintf("Uptime: %s\n", formatDuration(s.Uptime)))
	}

	if s.Hashrate != nil {
		sb.WriteString(fmt.Sprintf("Hashrate: %.2f H/s (10s) | %.2f H/s (60s) | %.2f H/s (max)\n",
			s.Hashrate.Current, s.Hashrate.Average, s.Hashrate.Max))
	}

	if s.Pool != nil {
		sb.WriteString(fmt.Sprintf("Pool: %s\n", s.Pool.URL))
		if s.Pool.User != "" {
			// Truncate wallet address for display
			user := s.Pool.User
			if len(user) > 20 {
				user = user[:10] + "..." + user[len(user)-10:]
			}
			sb.WriteString(fmt.Sprintf("Wallet: %s\n", user))
		}
	}

	if s.DonateLevel > 0 {
		sb.WriteString(fmt.Sprintf("Donate Level: %d%%\n", s.DonateLevel))
	}

	return sb.String()
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

