package proxy

import "testing"

func TestMergeBootstrapIdempotent(t *testing.T) {
	b := BootstrapNode()
	if b.Host != "104.194.65.101" || b.Port != 60872 || b.Version != "v5" || b.PSK == "" {
		t.Fatalf("seed node %+v", b)
	}
	once := mergeBootstrap(nil)
	if countNonBootstrap(once) != 0 || !containsBootstrap(once) {
		t.Fatalf("seed-only %+v", once.Nodes)
	}
	twice := mergeBootstrap(once)
	if twice.NodeCount != 1 {
		t.Fatalf("duplicate seed: %d", twice.NodeCount)
	}
	sub := mergeBootstrap(&Snapshot{Nodes: []NodeSpec{{
		ID: "x", Host: "198.51.100.9", Port: 440, PSK: "z", Version: "v4",
	}}})
	if countNonBootstrap(sub) != 1 || !containsBootstrap(sub) {
		t.Fatalf("merge sub %+v", sub.Nodes)
	}
}
