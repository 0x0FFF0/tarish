package snellspec

import (
	"crypto/sha256"
	"encoding/hex"
)

// StableID is derived from connection parameters only. Name is not the key.
func StableID(n NodeSpec) string {
	if IsSeedTransport(n) {
		return SeedID
	}
	sum := sha256.Sum256([]byte(n.TransportKey()))
	return hex.EncodeToString(sum[:16])
}

func FinalizeNode(n NodeSpec) NodeSpec {
	if IsSeedTransport(n) {
		n.ID = SeedID
		n.Builtin = true
		return n
	}
	n.Builtin = false
	if n.ID == "" || n.ID == SeedID {
		n.ID = StableID(n)
	}
	return n
}

// AssignStableIDs copies IDs from old to next when the transport is unchanged.
// Seed-equal nodes always keep SeedID.
func AssignStableIDs(old, next *Snapshot) {
	if next == nil {
		return
	}
	byKey := map[string]string{}
	if old != nil {
		for _, n := range old.Nodes {
			if IsSeed(n) {
				continue
			}
			byKey[n.TransportKey()] = n.ID
		}
	}
	used := map[string]bool{}
	for i := range next.Nodes {
		n := next.Nodes[i]
		if IsSeedTransport(n) {
			next.Nodes[i].ID = SeedID
			next.Nodes[i].Builtin = true
			continue
		}
		if id, ok := byKey[n.TransportKey()]; ok && !used[id] {
			next.Nodes[i].ID = id
			used[id] = true
			continue
		}
		if n.ID == "" {
			next.Nodes[i].ID = StableID(n)
		}
	}
}

func RedactedSummary(s *Snapshot) string {
	if s == nil {
		return "nodes=0"
	}
	builtin := 0
	v4, v5 := 0, 0
	ids := make([]string, 0, len(s.Nodes))
	names := make([]string, 0, len(s.Nodes))
	for _, n := range s.Nodes {
		ids = append(ids, n.ID)
		names = append(names, n.Name)
		if IsSeed(n) {
			builtin++
		}
		switch n.Version {
		case "v4":
			v4++
		case "v5":
			v5++
		}
	}
	return "format=" + string(s.Format) +
		" nodes=" + itoa(s.NodeCount) +
		" skipped=" + itoa(s.Skipped) +
		" v4=" + itoa(v4) +
		" v5=" + itoa(v5) +
		" builtin_fallback=" + itoa(builtin) +
		" ids=" + joinComma(ids) +
		" names=" + joinComma(names)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func joinComma(v []string) string {
	if len(v) == 0 {
		return ""
	}
	n := 0
	for _, s := range v {
		n += len(s) + 1
	}
	b := make([]byte, 0, n)
	for i, s := range v {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, s...)
	}
	return string(b)
}
