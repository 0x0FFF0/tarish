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
	// macOS LaunchDaemon paths
	launchDaemonPath = "/Library/LaunchDaemons"
	launchDaemonName = "com.tarish.plist"

	// Linux systemd paths
	systemdPath    = "/etc/systemd/system"
	systemdService = "tarish.service"
)

// launchDaemonTemplate is the macOS LaunchDaemon plist template
const launchDaemonTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.tarish</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/tarish</string>
        <string>start</string>
        <string>--force</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <false/>
    <key>StandardOutPath</key>
    <string>/var/log/tarish.log</string>
    <key>StandardErrorPath</key>
    <string>/var/log/tarish.error.log</string>
</dict>
</plist>
`

// systemdTemplate is the Linux systemd unit file template
const systemdTemplate = `[Unit]
Description=Tarish Donate-free XMRig Manager
After=network.target

[Service]
Type=forking
ExecStart=/usr/local/bin/tarish start --force
ExecStop=/usr/local/bin/tarish stop
PIDFile=%s
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
`

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

// enableMacOS installs the LaunchDaemon on macOS
func enableMacOS() error {
	// Check if running as root
	if os.Geteuid() != 0 {
		return fmt.Errorf("enabling service requires root privileges. Run with sudo")
	}

	// Check if tarish is installed
	if _, err := os.Stat("/usr/local/bin/tarish"); os.IsNotExist(err) {
		return fmt.Errorf("tarish not installed. Run 'tarish install' first")
	}

	// Write plist file
	plistPath := filepath.Join(launchDaemonPath, launchDaemonName)
	if err := os.WriteFile(plistPath, []byte(launchDaemonTemplate), 0644); err != nil {
		return fmt.Errorf("failed to write LaunchDaemon: %w", err)
	}

	// Set correct ownership
	if err := exec.Command("chown", "root:wheel", plistPath).Run(); err != nil {
		return fmt.Errorf("failed to set ownership: %w", err)
	}

	// Load the daemon
	if err := exec.Command("launchctl", "load", "-w", plistPath).Run(); err != nil {
		return fmt.Errorf("failed to load LaunchDaemon: %w", err)
	}

	fmt.Println("Service enabled successfully")
	fmt.Println("Tarish will start automatically on boot")
	return nil
}

// disableMacOS removes the LaunchDaemon on macOS
func disableMacOS() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("disabling service requires root privileges. Run with sudo")
	}

	plistPath := filepath.Join(launchDaemonPath, launchDaemonName)

	// Check if service exists
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		fmt.Println("Service is not installed")
		return nil
	}

	// Unload the daemon
	exec.Command("launchctl", "unload", "-w", plistPath).Run()

	// Remove the plist file
	if err := os.Remove(plistPath); err != nil {
		return fmt.Errorf("failed to remove LaunchDaemon: %w", err)
	}

	fmt.Println("Service disabled successfully")
	return nil
}

// isEnabledMacOS checks if the LaunchDaemon is installed on macOS
func isEnabledMacOS() (bool, error) {
	plistPath := filepath.Join(launchDaemonPath, launchDaemonName)
	_, err := os.Stat(plistPath)
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

	// Check if tarish is installed
	if _, err := os.Stat("/usr/local/bin/tarish"); os.IsNotExist(err) {
		return fmt.Errorf("tarish not installed. Run 'tarish install' first")
	}

	// Get PID file path
	home := os.Getenv("HOME")
	if home == "" {
		home = "/root"
	}
	pidFile := filepath.Join(home, ".tarish", "xmrig.pid")

	// Write service file
	servicePath := filepath.Join(systemdPath, systemdService)
	serviceContent := fmt.Sprintf(systemdTemplate, pidFile)
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
