package service

import (
	"strings"
	"testing"
)

func TestGeneratedUnitsSuperviseMinerDaemon(t *testing.T) {
	if !strings.Contains(launchPlistTemplate, "_miner-daemon") {
		t.Fatal("launchd unit must start _miner-daemon")
	}
	if strings.Contains(launchPlistTemplate, "start") && strings.Contains(launchPlistTemplate, "--force") {
		t.Fatal("launchd still uses transient start --force")
	}
	if !strings.Contains(systemdTemplate, "_miner-daemon") {
		t.Fatal("systemd unit must start _miner-daemon")
	}
	if strings.Contains(systemdTemplate, "Type=forking") {
		t.Fatal("systemd should supervise the long-lived process")
	}
}
