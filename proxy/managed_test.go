package proxy

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"tarish/config"
	"tarish/snellspec"
)

func testNode(id, host, psk string, port int) snellspec.WireNode {
	n := snellspec.FinalizeNode(NodeSpec{
		ID:      id,
		Name:    "n",
		Host:    host,
		Port:    port,
		PSK:     psk,
		Version: "v5",
	})
	return snellspec.ToWireNode(n)
}

func testManagedDoc(ver int, nodes ...snellspec.WireNode) *snellspec.ManagedDocument {
	return &snellspec.ManagedDocument{
		ProtocolVersion: snellspec.ProtocolVersion,
		PolicyVersion:   ver,
		Mode:            snellspec.ModeInherit,
		Enabled:         true,
		Supported:       true,
		Nodes:           nodes,
		ValidatedAt:     time.Now(),
	}
}

func TestManagedAndLocalStoresDoNotOverwrite(t *testing.T) {
	testHome(t)
	store, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	localSnap, err := ParseDocument([]byte("L = snell, 198.51.100.8, 440, psk=localpsk, version=4\n"), FormatSurge)
	if err != nil {
		t.Fatal(err)
	}
	ps := &persistedStore{
		Format:         "surge",
		StaticDocument: true,
		FetchedAt:      now,
		ValidatedAt:    now,
		Snapshot:       snapshotPersist(localSnap),
	}
	if err := store.Save(ps); err != nil {
		t.Fatal(err)
	}
	doc := testManagedDoc(3, testNode("", "198.51.100.9", "managedpsk", 441))
	if _, err := ApplyManagedSnapshot(store, doc, ApplyHooks{}); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil || loaded == nil || loaded.Snapshot == nil {
		t.Fatalf("local store: %v", err)
	}
	if len(loaded.Snapshot.Nodes) == 0 || loaded.Snapshot.Nodes[0].PSK != "localpsk" {
		t.Fatalf("managed apply overwrote local subscription")
	}
	got, err := store.LoadManaged()
	if err != nil || got == nil || len(got.Nodes) != 1 || got.Nodes[0].PSK != "managedpsk" {
		t.Fatalf("managed store %+v err=%v", got, err)
	}
}

func TestApplySameVersionIsIdempotent(t *testing.T) {
	testHome(t)
	store, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	var replaces atomic.Int32
	hooks := ApplyHooks{
		ReplaceSnapshot: func(s *Snapshot) { replaces.Add(1) },
	}
	doc := testManagedDoc(4, testNode("", "198.51.100.9", "psk-a", 441))
	applied, err := ApplyManagedSnapshot(store, doc, hooks)
	if err != nil || !applied {
		t.Fatalf("first apply applied=%v err=%v", applied, err)
	}
	if replaces.Load() != 1 {
		t.Fatalf("replaces=%d", replaces.Load())
	}
	applied, err = ApplyManagedSnapshot(store, doc.Clone(), hooks)
	if err != nil || applied {
		t.Fatalf("second apply applied=%v err=%v", applied, err)
	}
	if replaces.Load() != 1 {
		t.Fatalf("same version called ReplaceSnapshot again: %d", replaces.Load())
	}
	body, _ := json.Marshal(doc)
	applied, err = ApplyPending(body, hooks)
	if err != nil || applied {
		t.Fatalf("pending same version applied=%v err=%v", applied, err)
	}
	if replaces.Load() != 1 {
		t.Fatalf("pending same version replaced: %d", replaces.Load())
	}
}

func TestApplyRejectsOlderVersion(t *testing.T) {
	testHome(t)
	store, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	newer := testManagedDoc(9, testNode("", "198.51.100.9", "psk-a", 441))
	if _, err := ApplyManagedSnapshot(store, newer, ApplyHooks{}); err != nil {
		t.Fatal(err)
	}
	older := testManagedDoc(8, testNode("", "198.51.100.9", "psk-b", 441))
	var replaces atomic.Int32
	applied, err := ApplyManagedSnapshot(store, older, ApplyHooks{
		ReplaceSnapshot: func(*Snapshot) { replaces.Add(1) },
	})
	if applied || ErrorCode(err) != "stale_version" {
		t.Fatalf("applied=%v err=%v", applied, err)
	}
	if replaces.Load() != 0 {
		t.Fatal("older version replaced snapshot")
	}
	got, _ := store.LoadManaged()
	if got.PolicyVersion != 9 || got.Nodes[0].PSK != "psk-a" {
		t.Fatalf("older version overwrote store: %+v", got)
	}
}

func TestApplyWriteFailureKeepsPrevious(t *testing.T) {
	testHome(t)
	store, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	first := testManagedDoc(1, testNode("", "198.51.100.9", "psk-keep", 441))
	if _, err := ApplyManagedSnapshot(store, first, ApplyHooks{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store.Dir(), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(store.Dir(), 0o700) })
	second := testManagedDoc(2, testNode("", "198.51.100.9", "psk-new", 441))
	var replaces atomic.Int32
	applied, err := ApplyManagedSnapshot(store, second, ApplyHooks{
		ReplaceSnapshot: func(*Snapshot) { replaces.Add(1) },
	})
	if applied || err == nil {
		t.Fatalf("expected write failure, applied=%v err=%v", applied, err)
	}
	if replaces.Load() != 0 {
		t.Fatal("failed write still replaced snapshot")
	}
	_ = os.Chmod(store.Dir(), 0o700)
	got, err := store.LoadManaged()
	if err != nil || got == nil || got.PolicyVersion != 1 || got.Nodes[0].PSK != "psk-keep" {
		t.Fatalf("previous snapshot lost: %+v err=%v", got, err)
	}
}

func TestCLIConfigureRefusesWhenManaged(t *testing.T) {
	testHome(t)
	store, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	localSnap, err := ParseDocument([]byte("L = snell, 198.51.100.8, 440, psk=clipsk, version=4\n"), FormatSurge)
	if err != nil {
		t.Fatal(err)
	}
	ps := &persistedStore{
		Format:         "surge",
		StaticDocument: true,
		FetchedAt:      now,
		ValidatedAt:    now,
		Snapshot:       snapshotPersist(localSnap),
	}
	if err := store.Save(ps); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyManagedSnapshot(store, testManagedDoc(1, testNode("", "198.51.100.9", "managedpsk", 441)), ApplyHooks{}); err != nil {
		t.Fatal(err)
	}
	in := strings.NewReader("N = snell, 198.51.100.5, 440, psk=newpsk, version=4\n")
	var out, errw bytes.Buffer
	err = HandleIO([]string{"configure", "--stdin"}, in, &out, &errw)
	if ErrorCode(err) != "managed_active" {
		t.Fatalf("err=%v out=%q errw=%q", err, out.String(), errw.String())
	}
	if !strings.Contains(errw.String(), "exit managed") {
		t.Fatalf("missing operator hint: %q", errw.String())
	}
	loaded, _ := store.Load()
	if loaded.Snapshot.Nodes[0].PSK != "clipsk" {
		t.Fatal("configure overwrote local file while managed")
	}
	if strings.Contains(errw.String(), "managedpsk") || strings.Contains(out.String(), "managedpsk") {
		t.Fatalf("leaked psk: %q %q", out.String(), errw.String())
	}
}

func TestExpiredManagedNotUsedForNewDial(t *testing.T) {
	testHome(t)
	writeMinerConfig(t)
	store, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	doc := testManagedDoc(2, testNode("", "198.51.100.1", "expiredpsk", 440))
	doc.ValidatedAt = time.Now().Add(-8 * 24 * time.Hour)
	if err := store.SaveManaged(doc); err != nil {
		t.Fatal(err)
	}
	if err := config.SetProxyEnabled(true); err != nil {
		t.Fatal(err)
	}
	miner := &recordingMiner{}
	d, _ := startTestDaemon(t, miner)
	if d.router == nil {
		t.Fatal("router missing")
	}
	for _, st := range d.router.nodes {
		if st.spec.Host == "198.51.100.1" {
			t.Fatal("expired managed node loaded for new connections")
		}
	}
	if _, ok := d.router.nodes[BootstrapNode().ID]; !ok {
		t.Fatal("expected seed after expired managed boot")
	}
}

func TestApplySameVersionDoesNotRestartMiner(t *testing.T) {
	testHome(t)
	writeMinerConfig(t)
	store, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	writeSnapshot(t, store, time.Now())
	if err := config.SetProxyEnabled(true); err != nil {
		t.Fatal(err)
	}
	miner := &recordingMiner{}
	d, _ := startTestDaemon(t, miner)
	starts := miner.Starts()
	if starts < 1 {
		t.Fatal("miner never started")
	}
	doc := testManagedDoc(5, testNode("", "198.51.100.9", "psk-a", 441))
	applied, err := ApplyManagedSnapshot(store, doc, ApplyHooks{
		NotifySupervisor: func() error {
			d.applyActiveSnapshot()
			return nil
		},
	})
	if err != nil || !applied {
		t.Fatalf("apply: %v %v", applied, err)
	}
	after := miner.Starts()
	applied, err = ApplyManagedSnapshot(store, doc.Clone(), ApplyHooks{
		NotifySupervisor: func() error {
			d.applyActiveSnapshot()
			return nil
		},
	})
	if err != nil || applied {
		t.Fatalf("second: %v %v", applied, err)
	}
	if miner.Starts() != after {
		t.Fatalf("same version restarted miner: before=%d after=%d now=%d", starts, after, miner.Starts())
	}
}

func TestApplyPendingJSONRoundTrip(t *testing.T) {
	testHome(t)
	doc := testManagedDoc(6, testNode("", "198.51.100.9", "psk-a", 441))
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "https://") {
		t.Fatal("pending body contained url")
	}
	var replaces atomic.Int32
	applied, err := ApplyPending(body, ApplyHooks{
		ReplaceSnapshot: func(*Snapshot) { replaces.Add(1) },
	})
	if err != nil || !applied {
		t.Fatalf("pending: %v %v", applied, err)
	}
	if replaces.Load() != 1 {
		t.Fatalf("replaces=%d", replaces.Load())
	}
}
