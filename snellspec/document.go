package snellspec

import "time"

const (
	ModeInherit = "inherit"
	ModeCustom  = "custom"
	ModeLocal   = "local"
)

// WireNode is the agent snapshot encoding of a node. Admin reads must redact PSK.
type WireNode struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	PSK      string `json:"psk,omitempty"`
	Version  string `json:"version"`
	ObfsMode string `json:"obfs_mode,omitempty"`
	ObfsHost string `json:"obfs_host,omitempty"`
	Builtin  bool   `json:"builtin,omitempty"`
}

func ToWireNode(n NodeSpec) WireNode {
	return WireNode{
		ID:       n.ID,
		Name:     n.Name,
		Host:     n.Host,
		Port:     n.Port,
		PSK:      n.PSK,
		Version:  n.Version,
		ObfsMode: n.ObfsMode,
		ObfsHost: n.ObfsHost,
		Builtin:  n.Builtin,
	}
}

func (w WireNode) Spec() NodeSpec {
	n := NodeSpec{
		ID:       w.ID,
		Name:     w.Name,
		Host:     w.Host,
		Port:     w.Port,
		PSK:      w.PSK,
		Version:  w.Version,
		ObfsMode: w.ObfsMode,
		ObfsHost: w.ObfsHost,
		Builtin:  w.Builtin,
	}
	return FinalizeNode(n)
}

func ToWireNodes(nodes []NodeSpec) []WireNode {
	out := make([]WireNode, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, ToWireNode(n))
	}
	return out
}

func SpecsFromWire(nodes []WireNode) []NodeSpec {
	out := make([]NodeSpec, 0, len(nodes))
	for _, w := range nodes {
		out = append(out, w.Spec())
	}
	return out
}

// ManagedDocument is the declarative snapshot pushed to miners.
// It never contains the upstream URL or token.
type ManagedDocument struct {
	ProtocolVersion int        `json:"protocol_version"`
	PolicyVersion   int        `json:"policy_version"`
	Mode            string     `json:"mode"`
	Enabled         bool       `json:"enabled"`
	Supported       bool       `json:"supported"`
	Nodes           []WireNode `json:"nodes"`
	ValidatedAt     time.Time  `json:"validated_at"`
	ExpiresAt       time.Time  `json:"expires_at,omitempty"`
	ErrorCode       string     `json:"error_code,omitempty"`
}

func (d *ManagedDocument) Snapshot() *Snapshot {
	if d == nil {
		return nil
	}
	nodes := SpecsFromWire(d.Nodes)
	return &Snapshot{
		Nodes:       nodes,
		NodeCount:   len(nodes),
		ValidatedAt: d.ValidatedAt,
		FetchedAt:   d.ValidatedAt,
	}
}

func (d *ManagedDocument) Clone() *ManagedDocument {
	if d == nil {
		return nil
	}
	cp := *d
	cp.Nodes = append([]WireNode(nil), d.Nodes...)
	return &cp
}
