package config

import "testing"

func TestConfigDirUsesTarishHomeOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TARISH_HOME", tmp)
	t.Setenv("TARISH_USER", "tester")
	t.Setenv("SUDO_USER", "")

	dir, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir() error = %v", err)
	}

	want := tmp + "/.local/share/tarish"
	if dir != want {
		t.Fatalf("ConfigDir() = %q, want %q", dir, want)
	}
}

func TestServerToggleKeepsStoredServerConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TARISH_HOME", tmp)
	t.Setenv("TARISH_USER", "tester")
	t.Setenv("SUDO_USER", "")

	if err := SetServerURL("https://miner.example"); err != nil {
		t.Fatalf("SetServerURL() error = %v", err)
	}
	if err := SetServerAgentKey("secret-agent-key"); err != nil {
		t.Fatalf("SetServerAgentKey() error = %v", err)
	}

	if !IsServerEnabled() {
		t.Fatal("IsServerEnabled() = false, want true when URL is configured and toggle is unset")
	}

	if err := SetServerEnabled(false); err != nil {
		t.Fatalf("SetServerEnabled(false) error = %v", err)
	}

	if IsServerEnabled() {
		t.Fatal("IsServerEnabled() = true, want false after explicit disable")
	}
	if got := GetServerURL(); got != "https://miner.example" {
		t.Fatalf("GetServerURL() = %q, want configured URL to remain stored", got)
	}
	if got := GetServerAgentKey(); got != "secret-agent-key" {
		t.Fatalf("GetServerAgentKey() = %q, want configured key to remain stored", got)
	}
}
