package xmrig

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"golang.org/x/mod/semver"
	"tarish/embedded"
)

// BinaryInfo holds information about an xmrig binary
type BinaryInfo struct {
	Path    string
	Version string
	OS      string
	Arch    string
}

// FindBinary finds the appropriate xmrig binary for the current system
func FindBinary(basePath string) (*BinaryInfo, error) {
	targetOS := runtime.GOOS
	targetArch := runtime.GOARCH

	// Map Go OS names to binary naming convention
	osName := targetOS
	if targetOS == "darwin" {
		osName = "macos"
	}

	// Expected binary name pattern: xmrig_{os}_{arch}
	expectedName := fmt.Sprintf("xmrig_%s_%s", osName, targetArch)

	// Find all version directories
	versions, err := findVersionDirs(basePath)
	if err != nil {
		return nil, fmt.Errorf("failed to scan binary directory: %w", err)
	}

	if len(versions) == 0 {
		return nil, fmt.Errorf("no xmrig versions found in %s", basePath)
	}

	// Sort versions in descending order (latest first)
	sort.Slice(versions, func(i, j int) bool {
		// Add 'v' prefix for semver comparison if not present
		vi := versions[i]
		vj := versions[j]
		if !strings.HasPrefix(vi, "v") {
			vi = "v" + vi
		}
		if !strings.HasPrefix(vj, "v") {
			vj = "v" + vj
		}
		return semver.Compare(vi, vj) > 0
	})

	// Try each version from latest to oldest
	for _, version := range versions {
		versionDir := filepath.Join(basePath, version)
		binaryPath := filepath.Join(versionDir, expectedName)

		if _, err := os.Stat(binaryPath); err == nil {
			return &BinaryInfo{
				Path:    binaryPath,
				Version: version,
				OS:      targetOS,
				Arch:    targetArch,
			}, nil
		}
	}

	return nil, fmt.Errorf("no compatible xmrig binary found for %s/%s in %s", targetOS, targetArch, basePath)
}

// findVersionDirs returns all version directories in the base path
func findVersionDirs(basePath string) ([]string, error) {
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return nil, err
	}

	var versions []string
	for _, entry := range entries {
		if entry.IsDir() {
			name := entry.Name()
			// Check if it looks like a version (starts with digit or 'v')
			if len(name) > 0 && (name[0] >= '0' && name[0] <= '9' || name[0] == 'v') {
				versions = append(versions, name)
			}
		}
	}

	return versions, nil
}

// GetInstalledBinaryPath returns the path to installed xmrig binary
func GetInstalledBinaryPath() (*BinaryInfo, error) {
	// 1. Check user-local path (~/.local/share/tarish/bin)
	home, _ := os.UserHomeDir()
	if home != "" {
		userPath := filepath.Join(home, ".local", "share", "tarish", "bin")
		info, err := FindBinary(userPath)
		if err == nil {
			return info, nil
		}
	}

	// 2. Check standard system installation path
	installPath := "/usr/local/share/tarish/bin"
	info, err := FindBinary(installPath)
	if err == nil {
		return info, nil
	}

	// Fallback to relative path (for development)
	execPath, err := os.Executable()
	if err == nil {
		execDir := filepath.Dir(execPath)
		devPath := filepath.Join(execDir, "bin")
		info, err = FindBinary(devPath)
		if err == nil {
			return info, nil
		}
	}

	// Try current working directory
	cwd, err := os.Getwd()
	if err == nil {
		cwdPath := filepath.Join(cwd, "bin")
		info, err = FindBinary(cwdPath)
		if err == nil {
			return info, nil
		}
	}

	// Fallback: extract from embedded assets on-demand
	fmt.Println("  Extracting xmrig from embedded assets...")
	binaryPath, err := embedded.ExtractXmrigBinary("") // Uses default path
	if err != nil {
		return nil, fmt.Errorf("no xmrig binary found: %w", err)
	}

	// Construct info from extracted path
	version, _ := GetBinaryVersion(binaryPath)
	return &BinaryInfo{
		Path:    binaryPath,
		Version: version,
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
	}, nil
}

// GetBinaryVersion returns version info for a specific binary
func GetBinaryVersion(binaryPath string) (string, error) {
	// Extract version from path (parent directory name)
	dir := filepath.Dir(binaryPath)
	version := filepath.Base(dir)

	// Validate it looks like a version
	if len(version) > 0 && (version[0] >= '0' && version[0] <= '9' || version[0] == 'v') {
		return version, nil
	}

	return "unknown", nil
}

// EnsureExecutable ensures the binary has execute permissions
func EnsureExecutable(binaryPath string) error {
	info, err := os.Stat(binaryPath)
	if err != nil {
		return err
	}

	// Add execute permission for owner, group, and others
	mode := info.Mode()
	if mode&0111 == 0 {
		return os.Chmod(binaryPath, mode|0755)
	}

	return nil
}
