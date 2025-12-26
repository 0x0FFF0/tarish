package update

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// Primary download base URL
	baseURL = "https://file.aooo.nl/tarish"
)

// Version is set at build time
var Version = "dev"

// Update checks for updates and downloads the latest version
func Update() error {
	fmt.Println("Checking for updates...")

	// Get current version
	currentVersion := GetCurrentVersion()
	fmt.Printf("Current version: %s\n", currentVersion)

	// Check latest version from server
	latestVersion, err := getLatestVersion()
	if err != nil {
		fmt.Printf("Warning: could not check version: %v\n", err)
		fmt.Println("Proceeding with download...")
		latestVersion = "latest"
	} else {
		fmt.Printf("Latest version: %s\n", latestVersion)

		// Compare versions
		if currentVersion == latestVersion && currentVersion != "dev" {
			fmt.Println("You are already running the latest version")
			return nil
		}
	}

	// Build download URL for current platform
	binaryName := getBinaryName()
	downloadURL := fmt.Sprintf("%s/dist/%s", baseURL, binaryName)

	fmt.Printf("Downloading %s...\n", binaryName)

	// Download to temp file
	tempFile, err := downloadFile(downloadURL)
	if err != nil {
		return fmt.Errorf("failed to download update: %w", err)
	}
	defer os.Remove(tempFile)

	// Replace current binary
	if err := replaceBinary(tempFile); err != nil {
		return fmt.Errorf("failed to install update: %w", err)
	}

	fmt.Println("Successfully updated tarish")
	return nil
}

// GetCurrentVersion returns the current version of tarish
func GetCurrentVersion() string {
	return Version
}

// getLatestVersion fetches the latest version from the server
func getLatestVersion() (string, error) {
	url := fmt.Sprintf("%s/version.txt", baseURL)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(body)), nil
}

// getBinaryName returns the expected binary name for current platform
func getBinaryName() string {
	osName := runtime.GOOS
	if osName == "darwin" {
		osName = "macos"
	}
	arch := runtime.GOARCH

	return fmt.Sprintf("tarish_%s_%s", osName, arch)
}

// downloadFile downloads a file to a temporary location
func downloadFile(url string) (string, error) {
	client := &http.Client{
		Timeout: 5 * time.Minute,
	}

	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// Create temp file
	tempFile, err := os.CreateTemp("", "tarish-update-*")
	if err != nil {
		return "", err
	}
	defer tempFile.Close()

	// Copy content
	written, err := io.Copy(tempFile, resp.Body)
	if err != nil {
		os.Remove(tempFile.Name())
		return "", err
	}

	fmt.Printf("Downloaded %d bytes\n", written)

	// Make executable
	if err := os.Chmod(tempFile.Name(), 0755); err != nil {
		os.Remove(tempFile.Name())
		return "", err
	}

	return tempFile.Name(), nil
}

// replaceBinary replaces the current binary with the new one
func replaceBinary(newBinaryPath string) error {
	// Get path to current executable
	currentPath, err := os.Executable()
	if err != nil {
		return err
	}

	// Resolve symlinks
	currentPath, err = filepath.EvalSymlinks(currentPath)
	if err != nil {
		return err
	}

	// Check if we have write permission
	info, err := os.Stat(currentPath)
	if err != nil {
		return err
	}

	// Create backup
	backupPath := currentPath + ".bak"
	if err := os.Rename(currentPath, backupPath); err != nil {
		return fmt.Errorf("failed to backup current binary: %w", err)
	}

	// Copy new binary to current location
	if err := copyFile(newBinaryPath, currentPath); err != nil {
		// Restore backup on failure
		os.Rename(backupPath, currentPath)
		return fmt.Errorf("failed to install new binary: %w", err)
	}

	// Set permissions
	if err := os.Chmod(currentPath, info.Mode()); err != nil {
		return err
	}

	// Remove backup
	os.Remove(backupPath)

	return nil
}

// copyFile copies a file from src to dst
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

// CheckForUpdates checks if an update is available without downloading
func CheckForUpdates() (bool, string, error) {
	latestVersion, err := getLatestVersion()
	if err != nil {
		return false, "", err
	}

	currentVersion := GetCurrentVersion()
	return currentVersion != latestVersion, latestVersion, nil
}
