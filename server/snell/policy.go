package snell

import (
	"time"

	"tarish/snellspec"
)

const Capability = "snell-managed"

type CatalogNode struct {
	Spec        snellspec.NodeSpec
	SourceID    string
	Manual      bool
	Enabled     bool
	ValidatedAt time.Time
}

type PoolConfig struct {
	IncludeSourceIDs    []string
	IncludeManualIDs    []string
	ManagedProxyEnabled bool
	PolicyVersion       int
}

type MinerPolicy struct {
	Mode            string
	Capable         bool
	Enabled         *bool
	CustomNodeIDs   []string
	ExpectedVersion int
}

func PoolValid(nodes []CatalogNode, pool PoolConfig) bool {
	return len(poolMembers(nodes, pool)) > 0
}

func poolMembers(nodes []CatalogNode, pool PoolConfig) []CatalogNode {
	src := setOf(pool.IncludeSourceIDs)
	man := setOf(pool.IncludeManualIDs)
	var out []CatalogNode
	for _, n := range nodes {
		if !n.Enabled || snellspec.IsSeed(n.Spec) {
			continue
		}
		if n.Manual {
			if man[n.Spec.ID] {
				out = append(out, n)
			}
			continue
		}
		if n.SourceID != "" && src[n.SourceID] {
			out = append(out, n)
		}
	}
	return out
}

func ComputeEffectiveSnapshot(nodes []CatalogNode, pool PoolConfig, miner MinerPolicy, now time.Time) *snellspec.ManagedDocument {
	doc := &snellspec.ManagedDocument{
		ProtocolVersion: snellspec.ProtocolVersion,
		PolicyVersion:   miner.ExpectedVersion,
		Mode:            miner.Mode,
		Supported:       miner.Capable,
	}
	if !miner.Capable {
		doc.Mode = ""
		doc.Supported = false
		return doc
	}
	if miner.Mode == "" {
		miner.Mode = snellspec.ModeLocal
		doc.Mode = snellspec.ModeLocal
	}
	if miner.Mode == snellspec.ModeLocal {
		doc.Enabled = false
		return doc
	}

	var selected []CatalogNode
	switch miner.Mode {
	case snellspec.ModeInherit:
		selected = poolMembers(nodes, pool)
		valid := PoolValid(nodes, pool)
		if miner.Enabled != nil {
			doc.Enabled = *miner.Enabled && valid
		} else {
			doc.Enabled = pool.ManagedProxyEnabled && valid
		}
	case snellspec.ModeCustom:
		want := setOf(miner.CustomNodeIDs)
		for _, n := range nodes {
			if !n.Enabled || snellspec.IsSeed(n.Spec) {
				continue
			}
			if want[n.Spec.ID] {
				selected = append(selected, n)
			}
		}
		if miner.Enabled != nil {
			doc.Enabled = *miner.Enabled
		} else {
			doc.Enabled = len(selected) > 0
		}
	default:
		doc.Mode = snellspec.ModeLocal
		doc.Enabled = false
		return doc
	}

	var validated time.Time
	wire := make([]snellspec.WireNode, 0, len(selected))
	for _, n := range selected {
		wire = append(wire, snellspec.ToWireNode(n.Spec))
		if validated.IsZero() || (!n.ValidatedAt.IsZero() && n.ValidatedAt.Before(validated)) {
			validated = n.ValidatedAt
		}
	}
	doc.Nodes = wire
	doc.ValidatedAt = validated
	if !validated.IsZero() {
		doc.ExpiresAt = validated.Add(snellspec.MaxCacheAge)
	}
	_ = now
	return doc
}

func setOf(ids []string) map[string]bool {
	m := map[string]bool{}
	for _, id := range ids {
		if id != "" {
			m[id] = true
		}
	}
	return m
}
