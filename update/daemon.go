package update

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"tarish/config"
)

// RunDaemon runs the auto-update check loop.  Blocks until killed or
// auto-update is disabled.  Intended to be invoked via the hidden
// "_update-daemon" command so that it runs as a detached background process.
func RunDaemon() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)

	fmt.Printf("[update-daemon] started (pid %d), checking every %v\n",
		os.Getpid(), config.GetCheckInterval())

	for {
		// Re-read interval each cycle so config edits take effect without restart.
		interval := config.GetCheckInterval()

		// Check if auto-update is still enabled.
		if !config.IsAutoUpdateEnabled() {
			fmt.Println("[update-daemon] auto-update disabled, exiting")
			return
		}

		// Perform the update check.
		result := AutoUpdate()
		switch result {
		case AutoUpdateApplied:
			config.RecordCheck()
			fmt.Println("[update-daemon] update applied, active on next tarish invocation")
		case AutoUpdateNoChange:
			config.RecordCheck()
		case AutoUpdateFailed:
			fmt.Println("[update-daemon] update failed, will retry next cycle")
		case AutoUpdateCheckErr:
			fmt.Println("[update-daemon] version check failed, will retry next cycle")
		case AutoUpdateSkipped:
			// dev build – nothing to do
		}

		// Sleep until next cycle or signal.
		select {
		case <-sig:
			fmt.Println("[update-daemon] received signal, shutting down")
			return
		case <-time.After(interval):
			// next iteration
		}
	}
}

// StartDaemon spawns the update daemon as a background process.
// No-op if the daemon is already running.
func StartDaemon() error {
	if _, running := IsDaemonRunning(); running {
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot locate tarish binary: %w", err)
	}
	exe, _ = filepath.EvalSymlinks(exe)

	logDir := daemonLogDir()
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("cannot create log dir: %w", err)
	}

	logPath := filepath.Join(logDir, "update-daemon.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("cannot open daemon log: %w", err)
	}

	cmd := exec.Command(exe, "_update-daemon")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("failed to start update daemon: %w", err)
	}

	if err := saveDaemonPID(cmd.Process.Pid); err != nil {
		cmd.Process.Kill()
		logFile.Close()
		return err
	}

	// Detach – let the daemon run independently.
	go func() {
		cmd.Wait()
		logFile.Close()
		os.Remove(daemonPIDFile())
	}()

	return nil
}

// StopDaemon sends SIGTERM to the update daemon (if running).
func StopDaemon() {
	pid, running := IsDaemonRunning()
	if !running {
		return
	}
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Signal(syscall.SIGTERM)
		time.Sleep(200 * time.Millisecond)
		if isProcessAlive(pid) {
			_ = p.Signal(syscall.SIGKILL)
		}
	}
	os.Remove(daemonPIDFile())
}

// IsDaemonRunning reports the PID and whether the update daemon is alive.
func IsDaemonRunning() (int, bool) {
	data, err := os.ReadFile(daemonPIDFile())
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false
	}
	return pid, isProcessAlive(pid)
}

// ---------- internal helpers ----------

func daemonPIDFile() string {
	dir, err := config.ConfigDir()
	if err != nil {
		return "/tmp/tarish-update-daemon.pid"
	}
	return filepath.Join(dir, "update-daemon.pid")
}

func daemonLogDir() string {
	dir, err := config.ConfigDir()
	if err != nil {
		return "/tmp"
	}
	return filepath.Join(dir, "log")
}

func saveDaemonPID(pid int) error {
	path := daemonPIDFile()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.Itoa(pid)), 0644)
}

func isProcessAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
