package store

import (
	"testing"
	"time"

	"tarish-server/models"
)

func TestResolveMinerIDSkipsUnknownWorkerID(t *testing.T) {
	id := ResolveMinerID(&models.AgentReport{
		MinerID:  "amd-0",
		WorkerID: "unknown",
		Hostname: "geno9b14",
		IP:       "192.168.10.26",
	})
	if id != "192-168-10-26" {
		t.Fatalf("id=%q want 192-168-10-26", id)
	}
}

func TestReportSnellCapableFromVersion(t *testing.T) {
	if reportSnellCapable(&models.AgentReport{TarishVersion: "v1.0.23"}) {
		t.Fatal("v1.0.23 should not be capable")
	}
	if !reportSnellCapable(&models.AgentReport{TarishVersion: "v1.1.0"}) {
		t.Fatal("v1.1.0 should be capable")
	}
	if !reportSnellCapable(&models.AgentReport{TarishVersion: "v1.1.0-overnight"}) {
		t.Fatal("v1.1.0-overnight should be capable")
	}
	if !reportSnellCapable(&models.AgentReport{Capabilities: []string{"snell-managed"}}) {
		t.Fatal("capability flag should be capable")
	}
}

func TestUpsertUnknownWorkerUsesIPAndCanEnroll(t *testing.T) {
	s := newTestStore(t)
	err := s.UpsertMiner(&models.AgentReport{
		MinerID:       "amd-0",
		WorkerID:      "unknown",
		Hostname:      "geno9b14",
		IP:            "192.168.10.26",
		TarishVersion: "v1.1.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	view, err := s.GetMinerSnell("192-168-10-26")
	if err != nil {
		t.Fatal(err)
	}
	if !view.Capable {
		t.Fatal("expected capable from version")
	}
	if view.Mode != "inherit" && view.Mode != "local" {
		t.Fatalf("mode %s", view.Mode)
	}
}

func TestUpsertMigratesPlaceholderUnknownID(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.Exec(`INSERT INTO miners (id, miner_id, worker_id, hostname, ip, last_seen) VALUES ('unknown', 'amd-0', 'unknown', 'geno9b14', '192.168.10.26', ?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO miner_snell (miner_id, mode, capable) VALUES ('unknown', 'local', 0)`); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMiner(&models.AgentReport{
		MinerID: "amd-0", WorkerID: "unknown", Hostname: "geno9b14", IP: "192.168.10.26",
		TarishVersion: "v1.1.0",
	}); err != nil {
		t.Fatal(err)
	}
	miners, err := s.GetMiners()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range miners {
		if m.ID == "unknown" {
			t.Fatal("placeholder unknown id still present")
		}
	}
	if _, err := s.GetMiner("192-168-10-26"); err != nil {
		t.Fatal(err)
	}
}
