package proxy

import "tarish/snellspec"

// Built-in seed node used when direct HTTPS subscription fetch is blocked
// (common on filtered networks). It is also kept as a route fallback so
// mining can still start before a full node list arrives.
//
// Display name is intentionally omitted from this struct; public status
// only exposes the opaque ID. Panel shows it as 内置兜底, out of the pool.
const bootstrapNodeID = snellspec.SeedID

func BootstrapNode() NodeSpec {
	return snellspec.SeedNode()
}

func mergeBootstrap(snap *Snapshot) *Snapshot {
	b := BootstrapNode()
	RememberSecrets(&Snapshot{Nodes: []NodeSpec{b}}, "")
	if snap == nil {
		return &Snapshot{
			Nodes:     []NodeSpec{b},
			NodeCount: 1,
			Format:    FormatSurge,
		}
	}
	for _, n := range snap.Nodes {
		if n.ID == b.ID || n.TransportEqual(b) {
			snap.NodeCount = len(snap.Nodes)
			return snap
		}
	}
	snap.Nodes = append([]NodeSpec{b}, snap.Nodes...)
	snap.NodeCount = len(snap.Nodes)
	return snap
}

func isBootstrap(n NodeSpec) bool {
	return snellspec.IsSeed(n)
}

func countNonBootstrap(snap *Snapshot) int {
	if snap == nil {
		return 0
	}
	n := 0
	for _, node := range snap.Nodes {
		if isBootstrap(node) {
			continue
		}
		n++
	}
	return n
}
