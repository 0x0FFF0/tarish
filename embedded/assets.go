package embedded

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// Assets is the embedded filesystem, set from main package
var Assets embed.FS

const (
	// Installation path for extracted assets
	SharePath = "/usr/local/share/tarish"
)

// ExtractAssets extracts all embedded assets to the share directory
func ExtractAssets(destPath string) error {
	if destPath == "" {
		destPath = SharePath
	}

	// Extract bin directory
	if err := extractDir("bin", destPath); err != nil {
		return fmt.Errorf("failed to extract bin: %w", err)
	}

	// Extract configs directory
	if err := extractDir("configs", destPath); err != nil {
		return fmt.Errorf("failed to extract configs: %w", err)
	}

	return nil
}

// ExtractXmrigBinary extracts only the xmrig binary for the current platform
func ExtractXmrigBinary(destPath string) (string, error) {
	if destPath == "" {
		destPath = SharePath
	}

	// Find the xmrig binary for current platform
	platformName := GetPlatformName()
	binaryName := "xmrig_" + platformName

	// Walk the bin directory to find the binary
	var foundPath string
	var foundVersion string

	err := fs.WalkDir(Assets, "bin", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == binaryName {
			foundPath = path
			// Extract version from path (bin/6.25.0/xmrig_...)
			dir := filepath.Dir(path)
			foundVersion = filepath.Base(dir)
		}
		return nil
	})

	if err != nil {
		return "", err
	}

	if foundPath == "" {
		return "", fmt.Errorf("xmrig binary not found for platform: %s", platformName)
	}

	// Create destination directory
	destDir := filepath.Join(destPath, "bin", foundVersion)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", err
	}

	// Extract the binary
	destFile := filepath.Join(destDir, binaryName)
	if err := extractFile(foundPath, destFile); err != nil {
		return "", err
	}

	// Make executable
	if err := os.Chmod(destFile, 0755); err != nil {
		return "", err
	}

	return destFile, nil
}

// ExtractConfigs extracts all config files
func ExtractConfigs(destPath string) error {
	if destPath == "" {
		destPath = SharePath
	}

	return extractDir("configs", destPath)
}

// GetEmbeddedConfig reads a config file directly from embedded assets
func GetEmbeddedConfig(name string) ([]byte, error) {
	path := filepath.Join("configs", name)
	return Assets.ReadFile(path)
}

// ListEmbeddedConfigs returns all embedded config file names
func ListEmbeddedConfigs() ([]string, error) {
	entries, err := Assets.ReadDir("configs")
	if err != nil {
		return nil, err
	}

	var configs []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			configs = append(configs, entry.Name())
		}
	}

	return configs, nil
}

// GetPlatformName returns the platform identifier (e.g., "macos_arm64")
func GetPlatformName() string {
	osName := runtime.GOOS
	if osName == "darwin" {
		osName = "macos"
	}
	return fmt.Sprintf("%s_%s", osName, runtime.GOARCH)
}

// extractDir extracts an embedded directory to destination
func extractDir(srcDir, destBase string) error {
	return fs.WalkDir(Assets, srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip .DS_Store files
		if d.Name() == ".DS_Store" {
			return nil
		}

		destPath := filepath.Join(destBase, path)

		if d.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}

		return extractFile(path, destPath)
	})
}

// extractFile extracts a single file from embedded assets
func extractFile(srcPath, destPath string) error {
	// Read from embedded
	data, err := Assets.ReadFile(srcPath)
	if err != nil {
		return err
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	// Write to destination
	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return err
	}

	return nil
}

// IsExtracted checks if assets have been extracted to the share path
func IsExtracted() bool {
	binPath := filepath.Join(SharePath, "bin")
	configsPath := filepath.Join(SharePath, "configs")

	_, binErr := os.Stat(binPath)
	_, configsErr := os.Stat(configsPath)

	return binErr == nil && configsErr == nil
}
