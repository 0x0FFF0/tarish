package xmrig

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestStopKillsOwnedPIDOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TARISH_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("SUDO_USER", "")

	configDir := filepath.Join(home, ".local", "share", "tarish", "configs")
	logDir := filepath.Join(home, ".local", "share", "tarish", "log")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatal(err)
	}

	foreign := exec.Command("sleep", "30")
	foreign.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := foreign.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = foreign.Process.Kill()
		_, _ = foreign.Process.Wait()
	}()

	owned := exec.Command("sleep", "30")
	owned.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := owned.Start(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(GetPIDFile(), []byte(strconv.Itoa(owned.Process.Pid)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := StopOwned(owned.Process.Pid); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_, _ = owned.Process.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("owned process not stopped")
	}
	if err := foreign.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("foreign process was killed")
	}
}
