package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"tarish-server/models"
)

func TestUpsertMinerSeparatesMinersWithSameMinerID(t *testing.T) {
	s := newTestStore(t)

	reportA := testAgentReport("epyc-0", "192-168-10-36", "mz73-0074", "192.168.10.36", 100)
	reportB := testAgentReport("epyc-0", "192-168-10-210", "CT100", "192.168.10.210", 200)

	if err := s.UpsertMiner(reportA); err != nil {
		t.Fatalf("UpsertMiner(reportA): %v", err)
	}
	if err := s.UpsertMiner(reportB); err != nil {
		t.Fatalf("UpsertMiner(reportB): %v", err)
	}

	miners, err := s.GetMiners()
	if err != nil {
		t.Fatalf("GetMiners: %v", err)
	}
	if len(miners) != 2 {
		t.Fatalf("GetMiners len = %d, want 2", len(miners))
	}

	got := map[string]string{}
	for _, miner := range miners {
		got[miner.ID] = miner.Hostname
	}

	if got["192-168-10-36"] != "mz73-0074" {
		t.Fatalf("miner 192-168-10-36 = %q, want %q", got["192-168-10-36"], "mz73-0074")
	}
	if got["192-168-10-210"] != "CT100" {
		t.Fatalf("miner 192-168-10-210 = %q, want %q", got["192-168-10-210"], "CT100")
	}
}

func TestUpsertMinerMigratesLegacyMinerIDRow(t *testing.T) {
	s := newTestStore(t)

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.Exec(`
		INSERT INTO miners (id, miner_id, worker_id, hostname, ip, cpu_model, cpu_family,
			cores, os, arch, xmrig_version, tarish_version, uptime_seconds,
			hashrate_current, hashrate_average, hashrate_max, config_json, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "epyc-0", "epyc-0", "192-168-10-210", "CT100", "192.168.10.210", "AMD EPYC", "amd_epyc",
		64, "linux", "amd64", "6.25.0", "v1.0.14", 1200, 150, 140, 160, "{}", now); err != nil {
		t.Fatalf("seed legacy miner: %v", err)
	}

	if _, err := s.db.Exec(`
		INSERT INTO hashrate_history (miner_id, timestamp, current, average, max)
		VALUES (?, ?, ?, ?, ?)
	`, "epyc-0", now, 150, 140, 160); err != nil {
		t.Fatalf("seed legacy history: %v", err)
	}

	if _, err := s.db.Exec(`
		INSERT INTO config_overrides (miner_id, override_json, created_at)
		VALUES (?, ?, ?)
	`, "epyc-0", `{"cpu":{"max-threads-hint":75}}`, now); err != nil {
		t.Fatalf("seed legacy override: %v", err)
	}

	report := testAgentReport("epyc-0", "192-168-10-210", "CT100", "192.168.10.210", 200)
	if err := s.UpsertMiner(report); err != nil {
		t.Fatalf("UpsertMiner: %v", err)
	}

	miner, err := s.GetMiner("192-168-10-210")
	if err != nil {
		t.Fatalf("GetMiner(new id): %v", err)
	}
	if miner.Hostname != "CT100" {
		t.Fatalf("GetMiner(new id) hostname = %q, want %q", miner.Hostname, "CT100")
	}

	if _, err := s.GetMiner("epyc-0"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetMiner(legacy id) err = %v, want sql.ErrNoRows", err)
	}

	override, err := s.GetConfigOverride("192-168-10-210")
	if err != nil {
		t.Fatalf("GetConfigOverride(new id): %v", err)
	}
	if override == nil {
		t.Fatal("GetConfigOverride(new id) = nil, want override")
	}

	legacyOverride, err := s.GetConfigOverride("epyc-0")
	if err != nil {
		t.Fatalf("GetConfigOverride(legacy id): %v", err)
	}
	if legacyOverride != nil {
		t.Fatalf("GetConfigOverride(legacy id) = %v, want nil", legacyOverride)
	}

	history, err := s.GetHashrateHistory("192-168-10-210", time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatalf("GetHashrateHistory: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("GetHashrateHistory len = %d, want 2", len(history))
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()

	s, err := New(filepath.Join(t.TempDir(), "tarish.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
	return s
}

func testAgentReport(minerID, workerID, hostname, ip string, current float64) *models.AgentReport {
	return &models.AgentReport{
		MinerID:       minerID,
		WorkerID:      workerID,
		Hostname:      hostname,
		IP:            ip,
		CPUModel:      "AMD EPYC",
		CPUFamily:     "amd_epyc",
		Cores:         64,
		OS:            "linux",
		Arch:          "amd64",
		XmrigVersion:  "6.25.0",
		TarishVersion: "v1.0.17",
		UptimeSeconds: 3600,
		Hashrate: &models.HashrateData{
			Current: current,
			Average: current - 10,
			Max:     current + 10,
		},
	}
}
