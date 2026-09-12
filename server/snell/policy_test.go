package snell

import (
	"testing"
	"time"

	"tarish/snellspec"
)

func TestComputeEffectiveSnapshotModes(t *testing.T) {
	n1 := snellspec.FinalizeNode(snellspec.NodeSpec{Name: "a", Host: "198.51.100.1", Port: 440, PSK: "p1", Version: "v5"})
	n2 := snellspec.FinalizeNode(snellspec.NodeSpec{Name: "b", Host: "198.51.100.2", Port: 440, PSK: "p2", Version: "v5"})
	seed := snellspec.SeedNode()
	src := "src-1"
	nodes := []CatalogNode{
		{Spec: n1, SourceID: src, Enabled: true, ValidatedAt: time.Now().Add(-time.Hour)},
		{Spec: n2, Manual: true, Enabled: true, ValidatedAt: time.Now().Add(-time.Hour)},
		{Spec: seed, SourceID: src, Enabled: true, ValidatedAt: time.Now()},
	}
	pool := PoolConfig{IncludeSourceIDs: []string{src}, IncludeManualIDs: []string{n2.ID}, ManagedProxyEnabled: true, PolicyVersion: 4}

	inherit := ComputeEffectiveSnapshot(nodes, pool, MinerPolicy{Mode: snellspec.ModeInherit, Capable: true, ExpectedVersion: 4}, time.Now())
	if !inherit.Enabled || inherit.PolicyVersion != 4 || len(inherit.Nodes) != 2 {
		t.Fatalf("inherit %+v", inherit)
	}
	for _, n := range inherit.Nodes {
		if n.Builtin || n.ID == seed.ID {
			t.Fatal("seed in pool snapshot")
		}
	}

	custom := ComputeEffectiveSnapshot(nodes, pool, MinerPolicy{Mode: snellspec.ModeCustom, Capable: true, CustomNodeIDs: []string{n1.ID}, ExpectedVersion: 7}, time.Now())
	if custom.PolicyVersion != 7 || len(custom.Nodes) != 1 || custom.Nodes[0].ID != n1.ID {
		t.Fatalf("custom %+v", custom)
	}

	local := ComputeEffectiveSnapshot(nodes, pool, MinerPolicy{Mode: snellspec.ModeLocal, Capable: true, ExpectedVersion: 4}, time.Now())
	if local.Enabled || len(local.Nodes) != 0 || local.Mode != snellspec.ModeLocal {
		t.Fatalf("local %+v", local)
	}

	unsupported := ComputeEffectiveSnapshot(nodes, pool, MinerPolicy{Capable: false}, time.Now())
	if unsupported.Supported || len(unsupported.Nodes) != 0 {
		t.Fatalf("unsupported %+v", unsupported)
	}

	emptyPool := PoolConfig{PolicyVersion: 1, ManagedProxyEnabled: true}
	fresh := ComputeEffectiveSnapshot(nodes, emptyPool, MinerPolicy{Mode: snellspec.ModeInherit, Capable: true}, time.Now())
	if fresh.Enabled {
		t.Fatal("invalid pool should not auto-enable")
	}

	followOn := ComputeEffectiveSnapshot(nodes, pool, MinerPolicy{Mode: snellspec.ModeInherit, Capable: true}, time.Now())
	if !followOn.Enabled {
		t.Fatal("nil override should follow managed pool on")
	}
	poolOff := pool
	poolOff.ManagedProxyEnabled = false
	followOff := ComputeEffectiveSnapshot(nodes, poolOff, MinerPolicy{Mode: snellspec.ModeInherit, Capable: true}, time.Now())
	if followOff.Enabled {
		t.Fatal("nil override should follow managed pool off")
	}
	stuck := false
	over := ComputeEffectiveSnapshot(nodes, pool, MinerPolicy{Mode: snellspec.ModeInherit, Capable: true, Enabled: &stuck}, time.Now())
	if over.Enabled {
		t.Fatal("explicit inherit override off should win")
	}
}

func TestComputeEffectiveSnapshotDoesNotBorrowPoolVersion(t *testing.T) {
	n1 := snellspec.FinalizeNode(snellspec.NodeSpec{Name: "a", Host: "198.51.100.1", Port: 440, PSK: "p1", Version: "v5"})
	nodes := []CatalogNode{{Spec: n1, SourceID: "src-1", Enabled: true}}
	pool := PoolConfig{IncludeSourceIDs: []string{"src-1"}, ManagedProxyEnabled: true, PolicyVersion: 4}

	local := ComputeEffectiveSnapshot(nodes, pool, MinerPolicy{Mode: snellspec.ModeLocal, Capable: true, ExpectedVersion: 0}, time.Now())
	if local.PolicyVersion != 0 {
		t.Fatalf("local borrowed pool version: %d", local.PolicyVersion)
	}
	inherit := ComputeEffectiveSnapshot(nodes, pool, MinerPolicy{Mode: snellspec.ModeInherit, Capable: true, ExpectedVersion: 0}, time.Now())
	if inherit.PolicyVersion != 0 {
		t.Fatalf("inherit borrowed pool version: %d", inherit.PolicyVersion)
	}
}
