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

// Version is set at build time via -ldflags
var Version = "dev"

// Update checks for updates and downloads the latest version (interactive)
func Update() error {
	fmt.Println("Checking for updates...")

	currentVersion := GetCurrentVersion()
	fmt.Printf("Current version: %s\n", currentVersion)

	latestVersion, err := getLatestVersion()
	if err != nil {
		fmt.Printf("Warning: could not check version: %v\n", err)
		fmt.Println("Proceeding with download...")
		latestVersion = "latest"
	} else {
		fmt.Printf("Latest version: %s\n", latestVersion)

		if currentVersion == latestVersion && currentVersion != "dev" {
			fmt.Println("You are already running the latest version")
			return nil
		}
	}

	return downloadAndReplace()
}

// AutoUpdateResult represents the outcome of an auto-update attempt.
type AutoUpdateResult int

const (
	AutoUpdateNoChange AutoUpdateResult = iota // checked successfully, already up-to-date
	AutoUpdateApplied                          // successfully downloaded and replaced binary
	AutoUpdateFailed                           // update available but download/replace failed
	AutoUpdateSkipped                          // skipped (dev build)
	AutoUpdateCheckErr                         // could not reach version server
)

// AutoUpdate silently checks and updates if a new version is available.
// The caller should use the result to decide whether to record the check
// timestamp: record on NoChange/Applied so the cooldown starts; skip
// recording on Failed/CheckErr so the next invocation retries immediately.
func AutoUpdate() AutoUpdateResult {
	currentVersion := GetCurrentVersion()
	if currentVersion == "dev" {
		return AutoUpdateSkipped
	}

	latestVersion, err := getLatestVersion()
	if err != nil {
		return AutoUpdateCheckErr
	}

	if latestVersion == currentVersion {
		return AutoUpdateNoChange
	}

	fmt.Printf("Auto-updating tarish %s -> %s ...\n", currentVersion, latestVersion)

	if err := downloadAndReplace(); err != nil {
		fmt.Printf("Auto-update failed: %v (continuing)\n", err)
		return AutoUpdateFailed
	}

	fmt.Println("Auto-update complete. New version active on next invocation.")
	return AutoUpdateApplied
}

// downloadAndReplace fetches the platform binary and replaces the current one
func downloadAndReplace() error {
	binaryName := getBinaryName()
	downloadURL := fmt.Sprintf("%s/dist/%s", baseURL, binaryName)

	fmt.Printf("Downloading %s...\n", binaryName)

	tempFile, err := downloadFile(downloadURL)
	if err != nil {
		return fmt.Errorf("failed to download update: %w", err)
	}
	defer os.Remove(tempFile)

	if err := replaceBinary(tempFile); err != nil {
		return fmt.Errorf("failed to install update: %w", err)
	}

	fmt.Println("Successfully updated tarish")
	return nil
}

// GetCurrentVersion returns the compiled-in version
func GetCurrentVersion() string {
	return Version
}

// getLatestVersion fetches the version string from the remote
func getLatestVersion() (string, error) {
	url := fmt.Sprintf("%s/version", baseURL)

	client := &http.Client{
		Timeout: 10 * time.Second,
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

// CheckForUpdates checks if an update is available without downloading
func CheckForUpdates() (bool, string, error) {
	latestVersion, err := getLatestVersion()
	if err != nil {
		return false, "", err
	}

	currentVersion := GetCurrentVersion()
	return currentVersion != latestVersion && currentVersion != "dev", latestVersion, nil
}

// getBinaryName returns the expected binary name for current platform
func getBinaryName() string {
	osName := runtime.GOOS
	if osName == "darwin" {
		osName = "macos"
	}
	return fmt.Sprintf("tarish_%s_%s", osName, runtime.GOARCH)
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

	tempFile, err := os.CreateTemp("", "tarish-update-*")
	if err != nil {
		return "", err
	}
	defer tempFile.Close()

	written, err := io.Copy(tempFile, resp.Body)
	if err != nil {
		os.Remove(tempFile.Name())
		return "", err
	}

	fmt.Printf("Downloaded %d bytes\n", written)

	if err := os.Chmod(tempFile.Name(), 0755); err != nil {
		os.Remove(tempFile.Name())
		return "", err
	}

	return tempFile.Name(), nil
}

// replaceBinary replaces the current binary with the new one
func replaceBinary(newBinaryPath string) error {
	currentPath, err := os.Executable()
	if err != nil {
		return err
	}

	currentPath, err = filepath.EvalSymlinks(currentPath)
	if err != nil {
		return err
	}

	info, err := os.Stat(currentPath)
	if err != nil {
		return err
	}

	// Atomic replace: backup -> copy -> remove backup
	backupPath := currentPath + ".bak"
	if err := os.Rename(currentPath, backupPath); err != nil {
		return fmt.Errorf("failed to backup current binary: %w", err)
	}

	if err := copyFile(newBinaryPath, currentPath); err != nil {
		os.Rename(backupPath, currentPath) // restore on failure
		return fmt.Errorf("failed to install new binary: %w", err)
	}

	if err := os.Chmod(currentPath, info.Mode()); err != nil {
		return err
	}

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
