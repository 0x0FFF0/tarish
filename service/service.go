package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// macOS paths
	systemLaunchDaemonPath = "/Library/LaunchDaemons"
	userLaunchAgentPath    = "Library/LaunchAgents" // Relative to Home
	plistName              = "com.tarish.plist"

	// Linux systemd paths
	systemdPath    = "/etc/systemd/system"
	systemdService = "tarish.service"
)

// launchPlistTemplate is the macOS LaunchDaemon/Agent plist template
// %s placeholders: 1=binary path, 2=log path, 3=error log path, 4=working dir
const launchPlistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.tarish</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>start</string>
        <string>--force</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <false/>
    <key>StandardOutPath</key>
    <string>%s</string>
    <key>StandardErrorPath</key>
    <string>%s</string>
    <key>WorkingDirectory</key>
    <string>%s</string>
</dict>
</plist>
`

// systemdTemplate is the Linux systemd unit file template
// %s placeholders: 1=binary path, 2=binary path (stop), 3=PID file
const systemdTemplate = `[Unit]
Description=Tarish Donate-free XMRig Manager
After=network.target

[Service]
Type=forking
ExecStart=%s start --force
ExecStop=%s stop
PIDFile=%s
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
`

// getInstallPaths returns binary and share paths based on user/root
func getInstallPaths() (binPath, sharePath string) {
	if os.Geteuid() == 0 {
		return "/usr/local/bin/tarish", "/usr/local/share/tarish"
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "bin", "tarish"),
		filepath.Join(home, ".local", "share", "tarish")
}

// findTarishBinary finds the installed tarish binary
func findTarishBinary() (string, error) {
	// Check user path first
	home, _ := os.UserHomeDir()
	if home != "" {
		userBin := filepath.Join(home, ".local", "bin", "tarish")
		if _, err := os.Stat(userBin); err == nil {
			return userBin, nil
		}
	}

	// Check system path
	sysBin := "/usr/local/bin/tarish"
	if _, err := os.Stat(sysBin); err == nil {
		return sysBin, nil
	}

	return "", fmt.Errorf("tarish not installed. Run 'tarish install' first")
}

// findSharePath finds the share directory based on binary location
func findSharePath(binPath string) string {
	// If binary is in ~/.local/bin, share is ~/.local/share/tarish
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(binPath, filepath.Join(home, ".local")) {
		return filepath.Join(home, ".local", "share", "tarish")
	}
	// Otherwise use system path
	return "/usr/local/share/tarish"
}

// Enable installs and enables the auto-start service
func Enable() error {
	switch runtime.GOOS {
	case "darwin":
		return enableMacOS()
	case "linux":
		return enableLinux()
	default:
		return fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}
}

// Disable removes the auto-start service
func Disable() error {
	switch runtime.GOOS {
	case "darwin":
		return disableMacOS()
	case "linux":
		return disableLinux()
	default:
		return fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}
}

// IsEnabled checks if the service is enabled
func IsEnabled() (bool, error) {
	switch runtime.GOOS {
	case "darwin":
		return isEnabledMacOS()
	case "linux":
		return isEnabledLinux()
	default:
		return false, fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}
}

// getMacOSPlistPath returns the appropriate plist path based on permissions
func getMacOSPlistPath() (string, bool, error) {
	if os.Geteuid() == 0 {
		// Root: System Daemon
		return filepath.Join(systemLaunchDaemonPath, plistName), true, nil
	}

	// User: Launch Agent
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, err
	}
	agentDir := filepath.Join(home, userLaunchAgentPath)
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		return "", false, err
	}
	return filepath.Join(agentDir, plistName), false, nil
}

// enableMacOS installs the LaunchDaemon/Agent on macOS
func enableMacOS() error {
	// Find tarish binary
	binPath, err := findTarishBinary()
	if err != nil {
		return err
	}

	// Get share path based on binary location
	sharePath := findSharePath(binPath)
	logPath := filepath.Join(sharePath, "log", "tarish.log")
	errorLogPath := filepath.Join(sharePath, "log", "tarish.error.log")

	// Ensure log directory exists
	logDir := filepath.Join(sharePath, "log")
	os.MkdirAll(logDir, 0755)

	plistPath, isRoot, err := getMacOSPlistPath()
	if err != nil {
		return err
	}

	// Generate plist content with correct paths
	plistContent := fmt.Sprintf(launchPlistTemplate, binPath, logPath, errorLogPath, sharePath)

	// Write plist file
	if err := os.WriteFile(plistPath, []byte(plistContent), 0644); err != nil {
		return fmt.Errorf("failed to write plist: %w", err)
	}

	// Set correct ownership if root
	if isRoot {
		if err := exec.Command("chown", "root:wheel", plistPath).Run(); err != nil {
			return fmt.Errorf("failed to set ownership: %w", err)
		}
	}

	// Load the daemon/agent
	if err := exec.Command("launchctl", "load", "-w", plistPath).Run(); err != nil {
		return fmt.Errorf("failed to load service: %w", err)
	}

	if isRoot {
		fmt.Println("System service enabled successfully (LaunchDaemon)")
		fmt.Println("Tarish will start automatically on system boot")
	} else {
		fmt.Println("User service enabled successfully (LaunchAgent)")
		fmt.Println("Tarish will start automatically when you log in")
	}
	return nil
}

// disableMacOS removes the LaunchDaemon/Agent on macOS
func disableMacOS() error {
	plistPath, _, err := getMacOSPlistPath()
	if err != nil {
		return err
	}

	// Check if service exists
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		// Try checking the other location just in case
		if os.Geteuid() != 0 {
			sysPath := filepath.Join(systemLaunchDaemonPath, plistName)
			if _, err := os.Stat(sysPath); err == nil {
				return fmt.Errorf("system service found at %s. Run with sudo to disable", sysPath)
			}
		}
		fmt.Println("Service is not installed")
		return nil
	}

	// Unload the daemon
	exec.Command("launchctl", "unload", "-w", plistPath).Run()

	// Remove the plist file
	if err := os.Remove(plistPath); err != nil {
		return fmt.Errorf("failed to remove plist: %w", err)
	}

	fmt.Println("Service disabled successfully")
	return nil
}

// isEnabledMacOS checks if the service is installed on macOS
func isEnabledMacOS() (bool, error) {
	plistPath, _, err := getMacOSPlistPath()
	if err != nil {
		return false, err
	}

	_, err = os.Stat(plistPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// enableLinux installs the systemd service on Linux
func enableLinux() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("enabling service requires root privileges. Run with sudo")
	}

	// Find tarish binary
	binPath, err := findTarishBinary()
	if err != nil {
		return err
	}

	// Get share path and PID file
	sharePath := findSharePath(binPath)
	pidFile := filepath.Join(sharePath, "log", "xmrig.pid")

	// Write service file
	servicePath := filepath.Join(systemdPath, systemdService)
	serviceContent := fmt.Sprintf(systemdTemplate, binPath, binPath, pidFile)
	if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
		return fmt.Errorf("failed to write systemd service: %w", err)
	}

	// Reload systemd
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("failed to reload systemd: %w", err)
	}

	// Enable the service
	if err := exec.Command("systemctl", "enable", systemdService).Run(); err != nil {
		return fmt.Errorf("failed to enable service: %w", err)
	}

	fmt.Println("Service enabled successfully")
	fmt.Println("Tarish will start automatically on boot")
	fmt.Println("To start now, run: sudo systemctl start tarish")
	return nil
}

// disableLinux removes the systemd service on Linux
func disableLinux() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("disabling service requires root privileges. Run with sudo")
	}

	servicePath := filepath.Join(systemdPath, systemdService)

	// Check if service exists
	if _, err := os.Stat(servicePath); os.IsNotExist(err) {
		fmt.Println("Service is not installed")
		return nil
	}

	// Stop the service if running
	exec.Command("systemctl", "stop", systemdService).Run()

	// Disable the service
	exec.Command("systemctl", "disable", systemdService).Run()

	// Remove the service file
	if err := os.Remove(servicePath); err != nil {
		return fmt.Errorf("failed to remove systemd service: %w", err)
	}

	// Reload systemd
	exec.Command("systemctl", "daemon-reload").Run()

	fmt.Println("Service disabled successfully")
	return nil
}

// isEnabledLinux checks if the systemd service is enabled on Linux
func isEnabledLinux() (bool, error) {
	output, err := exec.Command("systemctl", "is-enabled", systemdService).Output()
	if err != nil {
		// Service not found or disabled
		return false, nil
	}
	return strings.TrimSpace(string(output)) == "enabled", nil
}

// GetServiceStatus returns the current service status
func GetServiceStatus() string {
	enabled, err := IsEnabled()
	if err != nil {
		return "unknown"
	}
	if enabled {
		return "enabled"
	}
	return "disabled"
}
