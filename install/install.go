package install

import (
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"tarish/embedded"
)

const (
	binaryName = "tarish"
)

// getInstallPaths returns the installation paths based on current user (root vs user)
func getInstallPaths() (string, string, error) {
	if os.Geteuid() == 0 {
		// Root: System-wide installation
		return "/usr/local/bin", "/usr/local/share/tarish", nil
	}

	// User: User-local installation
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	binPath := filepath.Join(home, ".local", "bin")
	sharePath := filepath.Join(home, ".local", "share", "tarish")

	return binPath, sharePath, nil
}

// Install installs tarish to the system
func Install() error {
	binPath, sharePath, err := getInstallPaths()
	if err != nil {
		return err
	}

	isRoot := os.Geteuid() == 0
	mode := "User"
	if isRoot {
		mode = "System"
	}
	fmt.Printf("Installing tarish (%s-wide)...\n", mode)

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

	// Create bin directory if it doesn't exist
	if err := os.MkdirAll(binPath, 0755); err != nil {
		return fmt.Errorf("failed to create bin directory: %w", err)
	}

	// Create share directory
	if err := os.MkdirAll(sharePath, 0755); err != nil {
		return fmt.Errorf("failed to create share directory: %w", err)
	}

	// Copy binary (skip if already there)
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

	// Create log directory
	logDir := filepath.Join(sharePath, "log")
	// If root, make it world-writable so users can write logs
	perm := os.FileMode(0755)
	if isRoot {
		perm = 0777
	}
	if err := os.MkdirAll(logDir, perm); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}
	if isRoot {
		os.Chmod(logDir, 0777)
	}
	fmt.Printf("  Created log directory at %s\n", logDir)

	// Create data directory for PID file etc
	home, _ := os.UserHomeDir()
	if home != "" {
		dataDir := filepath.Join(home, ".tarish")
		os.MkdirAll(dataDir, 0755)
	}

	fmt.Println("\nInstallation complete!")
	if !isRoot {
		// Warn if not in PATH
		path := os.Getenv("PATH")
		if !contains(path, binPath) {
			shell := os.Getenv("SHELL")
			profile := "~/.bashrc"
			if strings.Contains(shell, "zsh") {
				profile = "~/.zshrc"
			}
			
			fmt.Printf("\n\033[33mWarning: %s is not in your PATH.\033[0m\n", binPath)
			fmt.Printf("To use 'tarish' command, run:\n\n")
			fmt.Printf("  echo 'export PATH=\"$PATH:%s\"' >> %s\n", binPath, profile)
			fmt.Printf("  source %s\n", profile)
		}
	}
	fmt.Println("Run 'tarish help' to see available commands")
	return nil
}

// Uninstall removes tarish from the system
func Uninstall() error {
	binPath, sharePath, err := getInstallPaths()
	if err != nil {
		return err
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

	fmt.Println("\nUninstallation complete!")
	return nil
}

// Helper: check if path is in PATH
func contains(pathEnv, target string) bool {
	for _, p := range filepath.SplitList(pathEnv) {
		if p == target {
			return true
		}
	}
	return false
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
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		execCmd("pkill", "-9", "xmrig")
	}
}

// disableService disables the auto-start service
func disableService() {
	// This functionality is now handled in service/service.go
	// But we keep basic cleanup here just in case
	// Proper disable should be done via 'tarish service disable' before uninstall
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
	binPath, _, err := getInstallPaths()
	if err != nil {
		return false
	}
	binaryPath := filepath.Join(binPath, binaryName)
	_, err = os.Stat(binaryPath)
	return err == nil
}

// GetInstallPath returns the installation path
func GetInstallPath() string {
	binPath, _, _ := getInstallPaths()
	return filepath.Join(binPath, binaryName)
}

// GetSharePath returns the share path
func GetSharePath() string {
	_, sharePath, _ := getInstallPaths()
	return sharePath
}
