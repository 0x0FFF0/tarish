package antisleep

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Guard represents an anti-sleep guard that prevents system sleep
type Guard struct {
	cmd    *exec.Cmd
	mu     sync.Mutex
	active bool
}

var (
	globalGuard *Guard
	guardMu     sync.Mutex
)

// Enable prevents the system from going to sleep
// This function starts a background process that keeps the system awake
func Enable() error {
	guardMu.Lock()
	defer guardMu.Unlock()

	// If already enabled, do nothing
	if globalGuard != nil && globalGuard.active {
		return nil
	}

	guard := &Guard{}

	switch runtime.GOOS {
	case "darwin":
		if err := guard.enableMacOS(); err != nil {
			return fmt.Errorf("failed to enable sleep prevention on macOS: %w", err)
		}
	case "linux":
		if err := guard.enableLinux(); err != nil {
			return fmt.Errorf("failed to enable sleep prevention on Linux: %w", err)
		}
	default:
		return fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}

	globalGuard = guard
	return nil
}

// Disable allows the system to sleep normally again.
// It stops the in-process guard if present, and also kills any orphaned
// sleep-prevention process on the system (cross-process cleanup).
func Disable() error {
	guardMu.Lock()
	defer guardMu.Unlock()

	// In-process stop
	if globalGuard != nil {
		globalGuard.stop()
	}

	// Cross-process cleanup: kill the system-level process
	killSystemProcess()
	return nil
}

// killSystemProcess terminates sleep-prevention processes on the system.
func killSystemProcess() {
	switch runtime.GOOS {
	case "darwin":
		exec.Command("pkill", "-f", "caffeinate -dim").Run()
	case "linux":
		exec.Command("pkill", "-f", "systemd-inhibit.*tarish.*sleep").Run()
	}
}

// IsEnabled returns whether sleep prevention is currently active.
// It checks the in-process guard first, then falls back to detecting
// the actual system process (caffeinate / systemd-inhibit) so it works
// across separate tarish invocations (e.g. tarish status).
func IsEnabled() bool {
	guardMu.Lock()
	defer guardMu.Unlock()

	// In-process check (same process that called Enable)
	if globalGuard != nil {
		globalGuard.mu.Lock()
		active := globalGuard.active
		globalGuard.mu.Unlock()
		if active {
			return true
		}
	}

	// Cross-process check: detect the actual running process
	return isActiveOnSystem()
}

// isActiveOnSystem detects whether the sleep-prevention process is running
// on the system, regardless of which tarish invocation started it.
func isActiveOnSystem() bool {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("pgrep", "-f", "caffeinate -dim").Output()
		return err == nil && len(strings.TrimSpace(string(out))) > 0
	case "linux":
		out, err := exec.Command("pgrep", "-f", "systemd-inhibit.*tarish.*sleep").Output()
		return err == nil && len(strings.TrimSpace(string(out))) > 0
	}
	return false
}

// enableMacOS uses caffeinate to prevent system sleep on macOS
func (g *Guard) enableMacOS() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	// caffeinate options:
	// -d: prevent display from sleeping
	// -i: prevent system from idle sleeping
	// -m: prevent disk from idle sleeping
	// -s: prevent system from sleeping (only works when on AC power)
	// We use -dim to prevent all types of sleep
	cmd := exec.Command("caffeinate", "-dim")

	// Set process group so we can kill it cleanly
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start caffeinate: %w", err)
	}

	g.cmd = cmd
	g.active = true

	// Monitor process in background
	go func() {
		cmd.Wait()
		g.mu.Lock()
		g.active = false
		g.mu.Unlock()
	}()

	return nil
}

// enableLinux uses systemd-inhibit to prevent system sleep on Linux
func (g *Guard) enableLinux() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Check if systemd-inhibit is available
	if _, err := exec.LookPath("systemd-inhibit"); err != nil {
		// Fallback for systems without systemd
		return g.enableLinuxLegacy()
	}

	// systemd-inhibit options:
	// --what=idle:sleep:handle-lid-switch - prevent idle, sleep, and lid close actions
	// --who=tarish - identify the inhibitor
	// --why="Mining in progress" - reason for inhibition
	// --mode=block - block sleep completely
	// sleep infinity - keep the inhibitor alive without depending on stdin
	cmd := exec.Command(
		"systemd-inhibit",
		"--what=idle:sleep:handle-lid-switch",
		"--who=tarish",
		"--why=Mining in progress - 24/7 operation required",
		"--mode=block",
		"sleep", "infinity",
	)

	// Detach from stdin so it works in service/non-interactive contexts
	cmd.Stdin = nil

	// Set process group for clean termination
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start systemd-inhibit: %w", err)
	}

	g.cmd = cmd
	g.active = true

	// Verify it didn't exit immediately (e.g. D-Bus/polkit failure)
	exited := make(chan error, 1)
	go func() {
		exited <- cmd.Wait()
	}()

	select {
	case err := <-exited:
		// Process exited right away -- systemd-inhibit failed
		g.active = false
		g.cmd = nil
		if err != nil {
			return fmt.Errorf("systemd-inhibit exited immediately: %w", err)
		}
		return fmt.Errorf("systemd-inhibit exited immediately")
	case <-time.After(250 * time.Millisecond):
		// Still running after 250ms -- good, it's holding the lock
	}

	// Monitor for later exit in background
	go func() {
		<-exited
		g.mu.Lock()
		g.active = false
		g.mu.Unlock()
	}()

	return nil
}

// enableLinuxLegacy is a fallback method for systems without systemd
// Note: This method is called with g.mu already locked from enableLinux
func (g *Guard) enableLinuxLegacy() error {
	// Unlock temporarily for the exec commands
	g.mu.Unlock()
	defer g.mu.Lock()

	// Try to use xset to disable display sleep (if X11 is available)
	if _, err := exec.LookPath("xset"); err == nil {
		// Disable DPMS (Display Power Management Signaling)
		exec.Command("xset", "s", "off").Run()
		exec.Command("xset", "-dpms").Run()
		exec.Command("xset", "s", "noblank").Run()
	}

	// Keep a dummy sleep process running
	cmd := exec.Command("sh", "-c", "while true; do sleep 3600; done")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start legacy sleep prevention: %w", err)
	}

	g.cmd = cmd
	g.active = true

	go func() {
		cmd.Wait()
		g.mu.Lock()
		g.active = false
		g.mu.Unlock()
	}()

	// Note: This is a best-effort approach for legacy systems
	// systemd-inhibit is the recommended method for modern Linux
	return nil
}

// stop terminates the anti-sleep guard process
func (g *Guard) stop() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.active || g.cmd == nil || g.cmd.Process == nil {
		return nil
	}

	// Kill the process group
	pgid, err := syscall.Getpgid(g.cmd.Process.Pid)
	if err == nil {
		// Kill entire process group
		syscall.Kill(-pgid, syscall.SIGTERM)
	} else {
		// Fallback: kill just the process
		g.cmd.Process.Kill()
	}

	g.active = false

	// Re-enable display sleep on Linux if xset was used
	if runtime.GOOS == "linux" {
		if _, err := exec.LookPath("xset"); err == nil {
			exec.Command("xset", "s", "on").Run()
			exec.Command("xset", "+dpms").Run()
		}
	}

	return nil
}
