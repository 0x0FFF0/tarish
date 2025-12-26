package install

import (
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"path/filepath"
	"runtime"

	"tarish/embedded"
)

const (
	// Installation paths
	binPath    = "/usr/local/bin"
	sharePath  = "/usr/local/share/tarish"
	binaryName = "tarish"
)

// Install installs tarish to the system
func Install() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("installation requires root privileges. Run with sudo")
	}

	fmt.Println("Installing tarish...")

	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Resolve symlinks
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	// Create share directory
	if err := os.MkdirAll(sharePath, 0755); err != nil {
		return fmt.Errorf("failed to create share directory: %w", err)
	}

	// Copy binary to /usr/local/bin (skip if already there)
	destBinary := filepath.Join(binPath, binaryName)
	if execPath != destBinary {
		if err := copyFile(execPath, destBinary); err != nil {
			return fmt.Errorf("failed to copy binary: %w", err)
		}
		fmt.Printf("  Installed binary to %s\n", destBinary)
	} else {
		fmt.Printf("  Binary already at %s\n", destBinary)
	}
	if err := os.Chmod(destBinary, 0755); err != nil {
		return fmt.Errorf("failed to set binary permissions: %w", err)
	}

	// Extract embedded assets (xmrig binaries and configs)
	fmt.Println("  Extracting embedded assets...")
	if err := embedded.ExtractAssets(sharePath); err != nil {
		return fmt.Errorf("failed to extract assets: %w", err)
	}

	// Make xmrig binaries executable
	binDir := filepath.Join(sharePath, "bin")
	filepath.Walk(binDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && filepath.Base(path) != ".DS_Store" {
			os.Chmod(path, 0755)
		}
		return nil
	})
	fmt.Printf("  Installed xmrig binaries to %s\n", binDir)
	fmt.Printf("  Installed configs to %s\n", filepath.Join(sharePath, "configs"))

	// Create log directory (world-writable so non-root users can write logs)
	logDir := filepath.Join(sharePath, "log")
	if err := os.MkdirAll(logDir, 0777); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}
	// Ensure it's writable even if it existed before
	os.Chmod(logDir, 0777)

	// Also ensure existing log file is writable
	logFile := filepath.Join(logDir, "xmrig.log")
	if _, err := os.Stat(logFile); err == nil {
		os.Chmod(logFile, 0666)
	}

	fmt.Printf("  Created log directory at %s\n", logDir)

	// Create data directory for PID file etc
	home := os.Getenv("HOME")
	if home == "" {
		home = "/root"
	}
	dataDir := filepath.Join(home, ".tarish")
	os.MkdirAll(dataDir, 0755)

	fmt.Println("\nInstallation complete!")
	fmt.Println("Run 'tarish help' to see available commands")
	return nil
}

// Uninstall removes tarish from the system
func Uninstall() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("uninstallation requires root privileges. Run with sudo")
	}

	fmt.Println("Uninstalling tarish...")

	// Stop any running processes first
	fmt.Println("  Stopping running processes...")
	stopXmrig()

	// Disable service if enabled
	fmt.Println("  Disabling service...")
	disableService()

	// Remove binary
	binaryPath := filepath.Join(binPath, binaryName)
	if err := os.Remove(binaryPath); err != nil && !os.IsNotExist(err) {
		fmt.Printf("  Warning: failed to remove binary: %v\n", err)
	} else {
		fmt.Printf("  Removed %s\n", binaryPath)
	}

	// Remove share directory
	if err := os.RemoveAll(sharePath); err != nil && !os.IsNotExist(err) {
		fmt.Printf("  Warning: failed to remove share directory: %v\n", err)
	} else {
		fmt.Printf("  Removed %s\n", sharePath)
	}

	// Optionally remove user data (ask first in real implementation)
	// For now, leave ~/.tarish intact

	fmt.Println("\nUninstallation complete!")
	fmt.Println("User data in ~/.tarish has been preserved")
	return nil
}

// copyFile copies a single file from src to dst
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// stopXmrig stops any running xmrig processes
func stopXmrig() {
	// Use pkill to stop xmrig processes
	// This is a simple implementation - the main code has better cleanup
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		// Just try to kill, ignore errors
		execCmd("pkill", "-9", "xmrig")
	}
}

// disableService disables the auto-start service
func disableService() {
	switch runtime.GOOS {
	case "darwin":
		plistPath := "/Library/LaunchDaemons/com.tarish.plist"
		execCmd("launchctl", "unload", "-w", plistPath)
		os.Remove(plistPath)
	case "linux":
		execCmd("systemctl", "stop", "tarish.service")
		execCmd("systemctl", "disable", "tarish.service")
		os.Remove("/etc/systemd/system/tarish.service")
		execCmd("systemctl", "daemon-reload")
	}
}

// execCmd runs a command silently
func execCmd(name string, args ...string) {
	cmd := execCommand(name, args...)
	if cmd != nil {
		cmd.Run()
	}
}

// execCommand is a helper to create exec.Command
func execCommand(name string, args ...string) *execCmdWrapper {
	return &execCmdWrapper{name: name, args: args}
}

// execCmdWrapper wraps command execution
type execCmdWrapper struct {
	name string
	args []string
}

// Run executes the command
func (c *execCmdWrapper) Run() error {
	cmd := osexec.Command(c.name, c.args...)
	return cmd.Run()
}

// IsInstalled checks if tarish is installed
func IsInstalled() bool {
	binaryPath := filepath.Join(binPath, binaryName)
	_, err := os.Stat(binaryPath)
	return err == nil
}

// GetInstallPath returns the installation path
func GetInstallPath() string {
	return filepath.Join(binPath, binaryName)
}

// GetSharePath returns the share path
func GetSharePath() string {
	return sharePath
}
