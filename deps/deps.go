package deps

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	randomXBoostURL  = "https://raw.githubusercontent.com/xmrig/xmrig/master/scripts/randomx_boost.sh"
	randomXBoostPath = "/usr/local/bin/randomx_boost.sh"
	msrServiceName   = "xmrig-msr.service"
	msrServicePath   = "/etc/systemd/system/xmrig-msr.service"
)

const msrServiceContent = `[Unit]
Description=Apply XMRig MSR Mods for RandomX
After=network.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/randomx_boost.sh
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
`

// Ensure verifies runtime dependencies before commands that need the mining
// runtime or service bootstrap.
func Ensure(command string, args []string) error {
	if !ShouldEnsure(command, args) {
		return nil
	}

	fmt.Println("Checking runtime dependencies...")

	switch runtime.GOOS {
	case "linux":
		if err := ensureLinux(); err != nil {
			return err
		}
	case "darwin":
		if err := ensureDarwin(); err != nil {
			return err
		}
	}

	fmt.Println("  Runtime dependencies ready")
	return nil
}

// ShouldEnsure returns true when the command path should block on dependency
// verification before proceeding.
func ShouldEnsure(command string, args []string) bool {
	switch strings.ToLower(command) {
	case "install", "i", "start", "st":
		return true
	case "service":
		return len(args) > 0 && strings.EqualFold(args[0], "enable")
	default:
		return false
	}
}

func ensureLinux() error {
	if err := ensureLinuxHwloc(); err != nil {
		return err
	}
	if err := ensureLinuxMSRTools(); err != nil {
		return err
	}
	if err := ensureRandomXBoostScript(); err != nil {
		return err
	}
	if err := ensureMSRService(); err != nil {
		return err
	}
	return nil
}

func ensureDarwin() error {
	if _, err := exec.LookPath("caffeinate"); err != nil {
		return fmt.Errorf("macOS sleep-prevention dependency 'caffeinate' is unavailable: %w", err)
	}

	if hasHwlocTool() {
		return nil
	}

	brewPath, ok := findBrew()
	if !ok {
		fmt.Println("  Homebrew not found; skipping optional macOS hwloc install")
		fmt.Println("  Linux-only MSR tuning is not applied on macOS")
		return nil
	}

	if os.Geteuid() == 0 {
		fmt.Println("  Homebrew detected, but tarish is running as root; skipping brew install for hwloc")
		fmt.Println("  Linux-only MSR tuning is not applied on macOS")
		return nil
	}

	if brewFormulaInstalled(brewPath, "hwloc") {
		return nil
	}

	fmt.Println("  Installing Homebrew formula: hwloc")
	if err := runCommand(brewPath, "install", "hwloc"); err != nil {
		return fmt.Errorf("failed to install hwloc with Homebrew: %w", err)
	}

	fmt.Println("  Linux-only MSR tuning is not applied on macOS")
	return nil
}

func ensureLinuxHwloc() error {
	if hasHwlocTool() && hasLinuxHwlocLibrary() {
		return nil
	}

	fmt.Println("  Installing Linux packages: libhwloc15, hwloc")
	if err := ensureAptPackages("libhwloc15", "hwloc"); err != nil {
		return fmt.Errorf("failed to install hwloc dependencies: %w", err)
	}

	if !hasHwlocTool() {
		return fmt.Errorf("hwloc tools are still unavailable after installation")
	}
	if !hasLinuxHwlocLibrary() {
		return fmt.Errorf("libhwloc runtime library is still unavailable after installation")
	}

	return nil
}

func ensureLinuxMSRTools() error {
	if commandAvailable("rdmsr") && commandAvailable("wrmsr") {
		return nil
	}

	fmt.Println("  Installing Linux package: msr-tools")
	if err := ensureAptPackages("msr-tools"); err != nil {
		return fmt.Errorf("failed to install msr-tools: %w", err)
	}

	if !commandAvailable("rdmsr") || !commandAvailable("wrmsr") {
		return fmt.Errorf("msr-tools commands are still unavailable after installation")
	}

	return nil
}

func ensureRandomXBoostScript() error {
	info, err := os.Stat(randomXBoostPath)
	if err == nil && info.Size() > 0 && info.Mode()&0111 != 0 {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to inspect %s: %w", randomXBoostPath, err)
	}

	fmt.Printf("  Installing %s\n", randomXBoostPath)
	tmpPath, err := downloadToTemp(randomXBoostURL, "tarish-randomx-boost-*.sh")
	if err != nil {
		return fmt.Errorf("failed to download randomx_boost.sh: %w", err)
	}
	defer os.Remove(tmpPath)

	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("failed to prepare randomx_boost.sh permissions: %w", err)
	}

	if err := installFile(tmpPath, randomXBoostPath, 0755); err != nil {
		return fmt.Errorf("failed to install randomx_boost.sh: %w", err)
	}

	return nil
}

func ensureMSRService() error {
	changed := false

	current, err := os.ReadFile(msrServicePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read %s: %w", msrServicePath, err)
	}
	if err != nil || !sameFileContent(current, []byte(msrServiceContent)) {
		fmt.Printf("  Writing %s\n", msrServicePath)
		tmpFile, err := os.CreateTemp("", "tarish-xmrig-msr-*.service")
		if err != nil {
			return fmt.Errorf("failed to create temporary service file: %w", err)
		}
		tmpPath := tmpFile.Name()
		if _, err := tmpFile.WriteString(msrServiceContent); err != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("failed to write temporary service file: %w", err)
		}
		if err := tmpFile.Close(); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("failed to finalize temporary service file: %w", err)
		}
		defer os.Remove(tmpPath)

		if err := installFile(tmpPath, msrServicePath, 0644); err != nil {
			return fmt.Errorf("failed to install %s: %w", msrServicePath, err)
		}
		changed = true
	}

	if !commandAvailable("systemctl") {
		return fmt.Errorf("systemctl is required to manage %s", msrServiceName)
	}

	if err := runPrivileged("systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("failed to reload systemd: %w", err)
	}

	enabled, err := systemdUnitState("is-enabled", msrServiceName)
	if err != nil {
		return fmt.Errorf("failed to inspect %s state: %w", msrServiceName, err)
	}
	if !enabled {
		fmt.Printf("  Enabling %s\n", msrServiceName)
		if err := runPrivileged("systemctl", "enable", msrServiceName); err != nil {
			return fmt.Errorf("failed to enable %s: %w", msrServiceName, err)
		}
	}

	active, err := systemdUnitState("is-active", msrServiceName)
	if err != nil {
		return fmt.Errorf("failed to inspect %s runtime state: %w", msrServiceName, err)
	}

	switch {
	case changed && active:
		fmt.Printf("  Restarting %s\n", msrServiceName)
		if err := runPrivileged("systemctl", "restart", msrServiceName); err != nil {
			return fmt.Errorf("failed to restart %s: %w", msrServiceName, err)
		}
	case !active:
		fmt.Printf("  Starting %s\n", msrServiceName)
		if err := runPrivileged("systemctl", "start", msrServiceName); err != nil {
			return fmt.Errorf("failed to start %s: %w", msrServiceName, err)
		}
	}

	return nil
}

func ensureAptPackages(packages ...string) error {
	if !commandAvailable("apt-get") {
		return fmt.Errorf("apt-get is required for automatic Linux dependency installation")
	}

	fmt.Println("  Running apt-get update")
	if err := runPrivileged("apt-get", "update"); err != nil {
		return err
	}

	args := append([]string{"install", "-y"}, packages...)
	return runPrivileged("apt-get", args...)
}

func hasHwlocTool() bool {
	return commandAvailable("lstopo-no-graphics") ||
		commandAvailable("lstopo") ||
		commandAvailable("hwloc-info")
}

func hasLinuxHwlocLibrary() bool {
	if commandAvailable("ldconfig") {
		out, err := exec.Command("ldconfig", "-p").Output()
		if err == nil && strings.Contains(string(out), "libhwloc.so") {
			return true
		}
	}

	patterns := []string{
		"/usr/lib/libhwloc.so*",
		"/usr/lib64/libhwloc.so*",
		"/usr/local/lib/libhwloc.so*",
		"/usr/local/lib64/libhwloc.so*",
		"/lib/libhwloc.so*",
		"/lib64/libhwloc.so*",
		"/usr/lib/x86_64-linux-gnu/libhwloc.so*",
		"/usr/lib/aarch64-linux-gnu/libhwloc.so*",
	}

	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		if len(matches) > 0 {
			return true
		}
	}

	return false
}

func findBrew() (string, bool) {
	if path, err := exec.LookPath("brew"); err == nil {
		return path, true
	}

	for _, candidate := range []string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true
		}
	}

	return "", false
}

func brewFormulaInstalled(brewPath, formula string) bool {
	return exec.Command(brewPath, "list", "--versions", formula).Run() == nil
}

func commandAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func downloadToTemp(url, pattern string) (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected HTTP status: %s", resp.Status)
	}

	tmpFile, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	tmpPath := tmpFile.Name()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return "", err
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return "", err
	}

	return tmpPath, nil
}

func installFile(src, dst string, mode os.FileMode) error {
	modeArg := fmt.Sprintf("%04o", mode.Perm())
	return runPrivileged("install", "-m", modeArg, src, dst)
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runPrivileged(name string, args ...string) error {
	cmd, err := privilegedCommand(name, args...)
	if err != nil {
		return err
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func privilegedCommand(name string, args ...string) (*exec.Cmd, error) {
	if os.Geteuid() == 0 {
		return exec.Command(name, args...), nil
	}
	if !commandAvailable("sudo") {
		return nil, fmt.Errorf("root privileges are required to run %s, but sudo is unavailable", name)
	}
	return exec.Command("sudo", append([]string{name}, args...)...), nil
}

func systemdUnitState(stateCmd, unit string) (bool, error) {
	cmd := exec.Command("systemctl", stateCmd, "--quiet", unit)
	err := cmd.Run()
	if err == nil {
		return true, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return false, nil
	}

	return false, err
}

func sameFileContent(current, desired []byte) bool {
	return strings.TrimSpace(string(current)) == strings.TrimSpace(string(desired))
}
