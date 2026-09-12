package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"tarish-server/models"
	"tarish/snellspec"
)

func TestMigrateExistingMinersStayLocal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE miners (
			id TEXT PRIMARY KEY,
			miner_id TEXT NOT NULL,
			worker_id TEXT NOT NULL,
			hostname TEXT DEFAULT '',
			ip TEXT DEFAULT '',
			cpu_model TEXT DEFAULT '',
			cpu_family TEXT DEFAULT '',
			cores INTEGER DEFAULT 0,
			os TEXT DEFAULT '',
			arch TEXT DEFAULT '',
			xmrig_version TEXT DEFAULT '',
			tarish_version TEXT DEFAULT '',
			uptime_seconds INTEGER DEFAULT 0,
			hashrate_current REAL DEFAULT 0,
			hashrate_average REAL DEFAULT 0,
			hashrate_max REAL DEFAULT 0,
			config_json TEXT DEFAULT '{}',
			last_seen DATETIME NOT NULL
		);
	`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO miners (id, miner_id, worker_id, hostname, last_seen) VALUES (?,?,?,?,?)`,
		"192-168-1-10", "m1", "192-168-1-10", "oldbox", now); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	view, err := s.GetMinerSnell("192-168-1-10")
	if err != nil {
		t.Fatal(err)
	}
	if view.Mode != snellspec.ModeLocal {
		t.Fatalf("migrated mode %s", view.Mode)
	}

	if err := s.UpsertMiner(&models.AgentReport{
		MinerID: "m1", WorkerID: "192-168-1-10", Hostname: "oldbox", IP: "192.168.1.10",
		Capabilities: []string{"snell-managed"},
	}); err != nil {
		t.Fatal(err)
	}
	view, err = s.GetMinerSnell("192-168-1-10")
	if err != nil {
		t.Fatal(err)
	}
	if view.Mode != snellspec.ModeLocal {
		t.Fatalf("heartbeat enrolled old miner: %s", view.Mode)
	}
	if !view.Capable {
		t.Fatal("expected capable flag")
	}
}

func TestNewCapableMinerInheritsWhenPoolValid(t *testing.T) {
	s := newTestStore(t)
	snap, err := snellspec.ParseDocument([]byte("A = snell, 198.51.100.1, 440, psk=p1, version=5\n"), snellspec.FormatSurge)
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource("panel", "https://example.invalid/sub", "surge")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PublishSourceSync(src.ID, snap, "", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdatePool([]string{src.ID}, nil, true); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMiner(&models.AgentReport{
		MinerID: "n1", WorkerID: "10-0-0-8", Hostname: "newbox", IP: "10.0.0.8",
		Capabilities: []string{"snell-managed"},
	}); err != nil {
		t.Fatal(err)
	}
	view, err := s.GetMinerSnell("10-0-0-8")
	if err != nil {
		t.Fatal(err)
	}
	if view.Mode != snellspec.ModeInherit {
		t.Fatalf("new miner mode %s", view.Mode)
	}
	if view.Enabled != nil {
		t.Fatalf("inherit enroll stored override %v", *view.Enabled)
	}
	snapOut, err := s.EffectiveSnapshot("10-0-0-8")
	if err != nil {
		t.Fatal(err)
	}
	if !snapOut.Enabled {
		t.Fatal("valid managed pool should enable inherit snapshot")
	}
}

func TestDefaultPoolEditBumpsOnlyInherit(t *testing.T) {
	s := newTestStore(t)
	for _, r := range []*models.AgentReport{
		{MinerID: "a", WorkerID: "w-a", Hostname: "a", Capabilities: []string{"snell-managed"}},
		{MinerID: "b", WorkerID: "w-b", Hostname: "b", Capabilities: []string{"snell-managed"}},
		{MinerID: "c", WorkerID: "w-c", Hostname: "c"},
	} {
		if err := s.UpsertMiner(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetMinerSnell("w-a", snellspec.ModeInherit, boolPtr(true), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMinerSnell("w-b", snellspec.ModeCustom, boolPtr(true), []string{"x"}); err != nil {
		t.Fatal(err)
	}
	beforeA, _ := s.GetMinerSnell("w-a")
	beforeB, _ := s.GetMinerSnell("w-b")
	beforeC, _ := s.GetMinerSnell("w-c")
	if _, err := s.UpdatePool([]string{"src"}, nil, true); err != nil {
		t.Fatal(err)
	}
	afterA, _ := s.GetMinerSnell("w-a")
	afterB, _ := s.GetMinerSnell("w-b")
	afterC, _ := s.GetMinerSnell("w-c")
	if afterA.ExpectedVersion <= beforeA.ExpectedVersion {
		t.Fatalf("inherit not bumped %d -> %d", beforeA.ExpectedVersion, afterA.ExpectedVersion)
	}
	if afterB.ExpectedVersion != beforeB.ExpectedVersion {
		t.Fatalf("custom bumped %d -> %d", beforeB.ExpectedVersion, afterB.ExpectedVersion)
	}
	if afterC.ExpectedVersion != beforeC.ExpectedVersion || afterC.Mode != snellspec.ModeLocal {
		t.Fatalf("local changed %+v", afterC)
	}
}

func TestBulkEnrollRestoreAndExit(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpsertMiner(&models.AgentReport{MinerID: "a", WorkerID: "w-a", Hostname: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMinerSnell("w-a", snellspec.ModeLocal, boolPtr(false), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.BulkEnroll([]string{"w-a"}); err != nil {
		t.Fatal(err)
	}
	enrolled, _ := s.GetMinerSnell("w-a")
	if enrolled.Mode != snellspec.ModeInherit {
		t.Fatalf("enrolled %s", enrolled.Mode)
	}
	if err := s.ExitManaged("w-a"); err != nil {
		t.Fatal(err)
	}
	restored, _ := s.GetMinerSnell("w-a")
	if restored.Mode != snellspec.ModeLocal {
		t.Fatalf("restored %s", restored.Mode)
	}
}

func TestInheritOverrideSurvivesStoreReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reopen.db")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := snellspec.ParseDocument([]byte("A = snell, 198.51.100.1, 440, psk=p1, version=5\n"), snellspec.FormatSurge)
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource("p", "https://example.invalid/s", "surge")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PublishSourceSync(src.ID, parsed, "", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdatePool([]string{src.ID}, nil, true); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMiner(&models.AgentReport{MinerID: "n", WorkerID: "w-ov", Hostname: "h", Capabilities: []string{"snell-managed"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMinerSnell("w-ov", snellspec.ModeInherit, boolPtr(false), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s2.Close() })
	view, err := s2.GetMinerSnell("w-ov")
	if err != nil {
		t.Fatal(err)
	}
	if view.Enabled == nil || *view.Enabled {
		t.Fatalf("override lost on reopen: %v", view.Enabled)
	}
	snap, err := s2.EffectiveSnapshot("w-ov")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Enabled {
		t.Fatal("explicit inherit override should stay off after reopen")
	}
}

func TestSetMinerSnellBumpsExpectedAbovePool(t *testing.T) {
	s := newTestStore(t)
	parsed, err := snellspec.ParseDocument([]byte("A = snell, 198.51.100.1, 440, psk=p1, version=5\nB = snell, 198.51.100.2, 440, psk=p2, version=5\n"), snellspec.FormatSurge)
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource("p", "https://example.invalid/s", "surge")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PublishSourceSync(src.ID, parsed, "", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdatePool([]string{src.ID}, nil, true); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMiner(&models.AgentReport{MinerID: "n", WorkerID: "w-bump", Hostname: "h", Capabilities: []string{"snell-managed"}}); err != nil {
		t.Fatal(err)
	}
	before, err := s.EffectiveSnapshot("w-bump")
	if err != nil {
		t.Fatal(err)
	}
	if before.PolicyVersion < 1 {
		t.Fatal("expected a policy version")
	}
	off := false
	if err := s.SetMinerSnell("w-bump", snellspec.ModeInherit, &off, nil); err != nil {
		t.Fatal(err)
	}
	after, err := s.EffectiveSnapshot("w-bump")
	if err != nil {
		t.Fatal(err)
	}
	if after.Enabled {
		t.Fatal("expected enabled=false")
	}
	if after.PolicyVersion <= before.PolicyVersion {
		t.Fatalf("expected_version not bumped: %d -> %d", before.PolicyVersion, after.PolicyVersion)
	}
	if _, err := s.UpdatePool([]string{src.ID}, nil, true); err != nil {
		t.Fatal(err)
	}
	catalog, err := s.EffectiveSnapshot("w-bump")
	if err != nil {
		t.Fatal(err)
	}
	if catalog.PolicyVersion < after.PolicyVersion {
		t.Fatalf("pool bump lowered miner expected %d -> %d", after.PolicyVersion, catalog.PolicyVersion)
	}
}

func TestInheritFollowsPoolAfterInvalidThenValid(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpsertMiner(&models.AgentReport{
		MinerID: "n", WorkerID: "w-follow", Hostname: "h",
		Capabilities: []string{"snell-managed"},
	}); err != nil {
		t.Fatal(err)
	}
	before, err := s.EffectiveSnapshot("w-follow")
	if err != nil {
		t.Fatal(err)
	}
	if before.Enabled {
		t.Fatal("invalid pool must not enable inherit")
	}
	view, _ := s.GetMinerSnell("w-follow")
	if view.Enabled != nil {
		t.Fatalf("enroll persisted override %v", *view.Enabled)
	}

	parsed, err := snellspec.ParseDocument([]byte("A = snell, 198.51.100.1, 440, psk=p1, version=5\n"), snellspec.FormatSurge)
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource("p", "https://example.invalid/s", "surge")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PublishSourceSync(src.ID, parsed, "", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdatePool([]string{src.ID}, nil, true); err != nil {
		t.Fatal(err)
	}
	on, err := s.EffectiveSnapshot("w-follow")
	if err != nil {
		t.Fatal(err)
	}
	if !on.Enabled {
		t.Fatal("managed pool on should enable inherit pending")
	}
	if _, err := s.UpdatePool([]string{src.ID}, nil, false); err != nil {
		t.Fatal(err)
	}
	off, err := s.EffectiveSnapshot("w-follow")
	if err != nil {
		t.Fatal(err)
	}
	if off.Enabled {
		t.Fatal("managed pool off should disable inherit pending")
	}
}

func TestDisableImportedNodeSurvivesResync(t *testing.T) {
	s := newTestStore(t)
	doc := []byte("A = snell, 198.51.100.1, 440, psk=p1, version=5\nB = snell, 198.51.100.2, 440, psk=p2, version=5\n")
	snap, err := snellspec.ParseDocument(doc, snellspec.FormatSurge)
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource("p", "https://example.invalid/s", "surge")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PublishSourceSync(src.ID, snap, "", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdatePool([]string{src.ID}, nil, true); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMiner(&models.AgentReport{MinerID: "n", WorkerID: "w-n", Hostname: "n", Capabilities: []string{"snell-managed"}}); err != nil {
		t.Fatal(err)
	}
	before, err := s.EffectiveSnapshot("w-n")
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Nodes) != 2 {
		t.Fatalf("nodes %d", len(before.Nodes))
	}
	off := false
	if err := s.UpdateNode(before.Nodes[0].ID, "", &off, "", 0, "", "", "", "", false); err != nil {
		t.Fatal(err)
	}
	again, err := snellspec.ParseDocument(doc, snellspec.FormatSurge)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PublishSourceSync(src.ID, again, "", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	after, err := s.EffectiveSnapshot("w-n")
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Nodes) != 1 {
		t.Fatalf("disabled node resurrected: %d", len(after.Nodes))
	}
	if after.Nodes[0].ID == before.Nodes[0].ID {
		t.Fatalf("disabled id %s still in snapshot", before.Nodes[0].ID)
	}
}

func TestDisableNodePublishesNewVersion(t *testing.T) {
	s := newTestStore(t)
	snap, err := snellspec.ParseDocument([]byte("A = snell, 198.51.100.1, 440, psk=p1, version=5\nB = snell, 198.51.100.2, 440, psk=p2, version=5\n"), snellspec.FormatSurge)
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource("p", "https://example.invalid/s", "surge")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PublishSourceSync(src.ID, snap, "", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdatePool([]string{src.ID}, nil, true); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMiner(&models.AgentReport{MinerID: "n", WorkerID: "w-n", Hostname: "n", Capabilities: []string{"snell-managed"}}); err != nil {
		t.Fatal(err)
	}
	before, _ := s.EffectiveSnapshot("w-n")
	if len(before.Nodes) != 2 {
		t.Fatalf("nodes %d", len(before.Nodes))
	}
	off := false
	if err := s.UpdateNode(before.Nodes[0].ID, "", &off, "", 0, "", "", "", "", false); err != nil {
		t.Fatal(err)
	}
	after, _ := s.EffectiveSnapshot("w-n")
	if after.PolicyVersion <= before.PolicyVersion {
		t.Fatalf("version not published %d -> %d", before.PolicyVersion, after.PolicyVersion)
	}
	if len(after.Nodes) != 1 {
		t.Fatalf("disabled node still in snapshot: %d", len(after.Nodes))
	}
}

func TestAckDoesNotRefreshValidatedAt(t *testing.T) {
	s := newTestStore(t)
	snap, err := snellspec.ParseDocument([]byte("A = snell, 198.51.100.1, 440, psk=p1, version=5\n"), snellspec.FormatSurge)
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.AddSource("p", "https://example.invalid/s", "surge")
	if err != nil {
		t.Fatal(err)
	}
	validated := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	if err := s.PublishSourceSync(src.ID, snap, "", "", validated); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSource(src.ID)
	if err != nil || got.ValidatedAt == nil {
		t.Fatal(err)
	}
	before := *got.ValidatedAt
	if err := s.AckSnell("nope", models.SnellAck{PolicyVersion: 1, OK: true}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetSource(src.ID)
	if !got.ValidatedAt.Equal(before) {
		t.Fatalf("ack refreshed validated_at")
	}
}

func boolPtr(v bool) *bool { return &v }
