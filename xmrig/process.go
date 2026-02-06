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

	"tarish/antisleep"
)

// ProcessStatus represents the current state of xmrig
type ProcessStatus struct {
	Running         bool
	PID             int
	Version         string
	Uptime          time.Duration
	Hashrate        *HashrateInfo
	Pool            *PoolInfo
	DonateLevel     int
	SleepPrevention bool
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

	// Ensure log directory exists and prepare log file
	if err := EnsureLogDir(); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}
	logFile := GetLogFile()
	// Open with 0666 permissions (read/write for everyone) so different users can append
	logHandle, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}
	// Explicitly chmod to ensure 0666 (OpenFile obeys umask)
	os.Chmod(logFile, 0666)

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
		// Disable sleep prevention when process exits
		antisleep.Disable()
	}()

	// Enable sleep prevention to keep system awake during mining
	if err := antisleep.Enable(); err != nil {
		fmt.Printf("Warning: Failed to enable sleep prevention: %v\n", err)
		fmt.Println("System may sleep during mining. Consider enabling manually.")
	} else {
		fmt.Println("Sleep prevention enabled - system will stay awake during mining")
	}

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

	// Disable sleep prevention
	if err := antisleep.Disable(); err != nil {
		fmt.Printf("Warning: Failed to disable sleep prevention: %v\n", err)
	} else if antisleep.IsEnabled() {
		// Only print if it was previously enabled
		fmt.Println("Sleep prevention disabled - system can sleep normally")
	}

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
	status.SleepPrevention = antisleep.IsEnabled()

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
	pidFile := GetPIDFile()
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0666); err != nil {
		return err
	}
	// Ensure world-writable
	return os.Chmod(pidFile, 0666)
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

// getAPIStatus tries to get status from xmrig's HTTP API.
// It reads the port and access-token from the active runtime config.
func getAPIStatus() (*APIResponse, error) {
	port, accessToken := GetHTTPConfigFromRuntime()

	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/1/summary", port)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API not available: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var apiResp APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &apiResp, nil
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

// ANSI color codes (consistent with printHelp in main.go)
const (
	colorCyan   = "\033[36m"
	colorYellow = "\033[33m"
	colorGreen  = "\033[32m"
	colorRed    = "\033[31m"
	colorGray   = "\033[90m"
	colorBold   = "\033[1m"
	colorReset  = "\033[0m"
)

// FormatStatus formats the status for display with color highlighting
func (s *ProcessStatus) FormatStatus() string {
	var sb strings.Builder

	if !s.Running {
		sb.WriteString(fmt.Sprintf("  %sStatus:           %s%sNOT RUNNING%s\n",
			colorYellow, colorReset, colorRed, colorReset))
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("  %sStatus:           %s%s%sRUNNING%s %s(PID: %d)%s\n",
		colorYellow, colorReset, colorBold, colorGreen, colorReset, colorGray, s.PID, colorReset))

	if s.Version != "" {
		sb.WriteString(fmt.Sprintf("  %sVersion:          %s%s%s%s\n",
			colorYellow, colorReset, colorCyan, s.Version, colorReset))
	}

	if s.Uptime > 0 {
		sb.WriteString(fmt.Sprintf("  %sUptime:           %s%s%s%s\n",
			colorYellow, colorReset, colorGreen, formatDuration(s.Uptime), colorReset))
	}

	if s.Hashrate != nil {
		sb.WriteString(fmt.Sprintf("  %sHashrate:         %s%s%s%.2f H/s%s %s(10s)%s | %s%.2f H/s%s %s(60s)%s | %s%.2f H/s%s %s(max)%s\n",
			colorYellow, colorReset,
			colorBold, colorGreen, s.Hashrate.Current, colorReset, colorGray, colorReset,
			colorGreen, s.Hashrate.Average, colorReset, colorGray, colorReset,
			colorGreen, s.Hashrate.Max, colorReset, colorGray, colorReset))
	}

	if s.Pool != nil {
		sb.WriteString(fmt.Sprintf("  %sPool:             %s%s%s%s\n",
			colorYellow, colorReset, colorCyan, s.Pool.URL, colorReset))
		if s.Pool.User != "" {
			user := s.Pool.User
			if len(user) > 20 {
				user = user[:10] + "..." + user[len(user)-10:]
			}
			sb.WriteString(fmt.Sprintf("  %sWallet:           %s%s%s%s\n",
				colorYellow, colorReset, colorCyan, user, colorReset))
		}
	}

	if s.DonateLevel > 0 {
		sb.WriteString(fmt.Sprintf("  %sDonate Level:     %s%s%d%%%s\n",
			colorYellow, colorReset, colorGray, s.DonateLevel, colorReset))
	}

	if s.SleepPrevention {
		sb.WriteString(fmt.Sprintf("  %sSleep Prevention: %s%s%sACTIVE ✓%s\n",
			colorYellow, colorReset, colorBold, colorGreen, colorReset))
	} else {
		sb.WriteString(fmt.Sprintf("  %sSleep Prevention: %s%sINACTIVE%s %s(restart with 'tarish start --force' to activate)%s\n",
			colorYellow, colorReset, colorRed, colorReset, colorGray, colorReset))
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
