package install

import (
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"path/filepath"
	"runtime"
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

	execDir := filepath.Dir(execPath)

	// Create share directory
	if err := os.MkdirAll(sharePath, 0755); err != nil {
		return fmt.Errorf("failed to create share directory: %w", err)
	}

	// Copy binary to /usr/local/bin
	destBinary := filepath.Join(binPath, binaryName)
	if err := copyFile(execPath, destBinary); err != nil {
		return fmt.Errorf("failed to copy binary: %w", err)
	}
	if err := os.Chmod(destBinary, 0755); err != nil {
		return fmt.Errorf("failed to set binary permissions: %w", err)
	}
	fmt.Printf("  Installed binary to %s\n", destBinary)

	// Copy bin/ directory (xmrig binaries)
	srcBin := filepath.Join(execDir, "bin")
	destBin := filepath.Join(sharePath, "bin")
	if _, err := os.Stat(srcBin); err == nil {
		if err := copyDir(srcBin, destBin); err != nil {
			return fmt.Errorf("failed to copy xmrig binaries: %w", err)
		}
		// Ensure xmrig binaries are executable
		filepath.Walk(destBin, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if !info.IsDir() && filepath.Base(path) != ".DS_Store" {
				os.Chmod(path, 0755)
			}
			return nil
		})
		fmt.Printf("  Installed xmrig binaries to %s\n", destBin)
	} else {
		fmt.Println("  Warning: xmrig binaries not found in source directory")
	}

	// Copy configs/ directory
	srcConfigs := filepath.Join(execDir, "configs")
	destConfigs := filepath.Join(sharePath, "configs")
	if _, err := os.Stat(srcConfigs); err == nil {
		if err := copyDir(srcConfigs, destConfigs); err != nil {
			return fmt.Errorf("failed to copy configs: %w", err)
		}
		fmt.Printf("  Installed configs to %s\n", destConfigs)
	} else {
		// Create empty configs directory
		os.MkdirAll(destConfigs, 0755)
		fmt.Printf("  Created configs directory at %s\n", destConfigs)
	}

	// Create data directory
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

// copyDir recursively copies a directory
func copyDir(src, dst string) error {
	// Create destination directory
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
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
