package update

import (
	"encoding/json"
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
	// Primary GitHub repo
	primaryRepo = "0x0FFF0/tarish"
	// Fallback URL (to be updated)
	fallbackURL = "https://example.com/tarish/releases"

	// GitHub API endpoints
	githubAPIBase = "https://api.github.com/repos"
)

// Release represents a GitHub release
type Release struct {
	TagName string  `json:"tag_name"`
	Name    string  `json:"name"`
	Assets  []Asset `json:"assets"`
}

// Asset represents a release asset
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// Update checks for updates and downloads the latest version
func Update() error {
	fmt.Println("Checking for updates...")

	// Get current version
	currentVersion := GetCurrentVersion()
	fmt.Printf("Current version: %s\n", currentVersion)

	// Get latest release info
	release, err := getLatestRelease()
	if err != nil {
		fmt.Println("Failed to check GitHub, trying fallback...")
		return updateFromFallback()
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	fmt.Printf("Latest version: %s\n", latestVersion)

	// Compare versions
	if currentVersion == latestVersion {
		fmt.Println("You are already running the latest version")
		return nil
	}

	// Find appropriate asset for current platform
	asset := findAssetForPlatform(release.Assets)
	if asset == nil {
		return fmt.Errorf("no compatible release found for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	fmt.Printf("Downloading %s (%d bytes)...\n", asset.Name, asset.Size)

	// Download to temp file
	tempFile, err := downloadAsset(asset.BrowserDownloadURL)
	if err != nil {
		return fmt.Errorf("failed to download update: %w", err)
	}
	defer os.Remove(tempFile)

	// Replace current binary
	if err := replaceBinary(tempFile); err != nil {
		return fmt.Errorf("failed to install update: %w", err)
	}

	fmt.Printf("Successfully updated to version %s\n", latestVersion)
	return nil
}

// GetCurrentVersion returns the current version of tarish
func GetCurrentVersion() string {
	// This will be set at build time using ldflags
	return Version
}

// Version is set at build time
var Version = "dev"

// getLatestRelease fetches the latest release from GitHub
func getLatestRelease() (*Release, error) {
	url := fmt.Sprintf("%s/%s/releases/latest", githubAPIBase, primaryRepo)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "tarish-updater")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	return &release, nil
}

// findAssetForPlatform finds the appropriate asset for the current OS/arch
func findAssetForPlatform(assets []Asset) *Asset {
	osName := runtime.GOOS
	if osName == "darwin" {
		osName = "macos"
	}
	arch := runtime.GOARCH

	// Expected naming: tarish_{os}_{arch}
	expectedName := fmt.Sprintf("tarish_%s_%s", osName, arch)

	for i := range assets {
		if strings.Contains(strings.ToLower(assets[i].Name), strings.ToLower(expectedName)) {
			return &assets[i]
		}
	}

	return nil
}

// downloadAsset downloads an asset to a temporary file
func downloadAsset(url string) (string, error) {
	client := &http.Client{
		Timeout: 5 * time.Minute, // Allow longer timeout for download
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

	// Copy with progress
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

	// On Unix, we can rename while running
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

// updateFromFallback attempts to update from the fallback URL
func updateFromFallback() error {
	// This is a placeholder - implement when fallback URL is configured
	return fmt.Errorf("fallback update not yet configured. Please download manually from GitHub")
}

// CheckForUpdates checks if an update is available without downloading
func CheckForUpdates() (bool, string, error) {
	release, err := getLatestRelease()
	if err != nil {
		return false, "", err
	}

	currentVersion := GetCurrentVersion()
	latestVersion := strings.TrimPrefix(release.TagName, "v")

	return currentVersion != latestVersion, latestVersion, nil
}

