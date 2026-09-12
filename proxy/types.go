package proxy

import (
	"fmt"
	"net"
	"strings"
	"time"

	"tarish/snellspec"
)

const (
	MaxDocumentBytes  = snellspec.MaxDocumentBytes
	MaxNodes          = snellspec.MaxNodes
	MaxYAMLDepth      = snellspec.MaxYAMLDepth
	FetchTimeout      = 15 * time.Second
	RefreshInterval   = 30 * time.Minute
	StaleAfter        = snellspec.StaleAfter
	MaxCacheAge       = snellspec.MaxCacheAge
	ProbeTimeout      = 5 * time.Second
	SelectBudget      = 15 * time.Second
	ProbeConcurrency  = 2
	CooldownStart     = 15 * time.Second
	CooldownCap       = 5 * time.Minute
	DirectProbeEvery  = 30 * time.Second
	RecoveryDirectMin = 2 * time.Minute
	RecoverySuccesses = 3
	HandshakeTimeout  = 5 * time.Second
	MaxSOCKSConns     = 64
)

type Format = snellspec.Format

const (
	FormatAuto   = snellspec.FormatAuto
	FormatSurge  = snellspec.FormatSurge
	FormatMihomo = snellspec.FormatMihomo
)

type RouteState string

const (
	RouteDisabled       RouteState = "disabled"
	RouteWaiting        RouteState = "waiting"
	RouteProxied        RouteState = "proxied"
	RouteDirectFallback RouteState = "direct-fallback"
)

// NodeSpec is private. It must not be logged or reported.
type NodeSpec = snellspec.NodeSpec
type Diagnostic = snellspec.Diagnostic
type Snapshot = snellspec.Snapshot

// NodeStatus is the public view of a node. No names, addresses, or secrets.
type NodeStatus struct {
	ID            string `json:"id"`
	Health        string `json:"health"`
	LastErrorCode string `json:"last_error_code,omitempty"`
	CooldownUntil string `json:"cooldown_until,omitempty"`
}

func snapshotPublicStatuses(s *Snapshot) []NodeStatus {
	if s == nil {
		return nil
	}
	out := make([]NodeStatus, 0, len(s.Nodes))
	for _, n := range s.Nodes {
		out = append(out, NodeStatus{ID: n.ID, Health: "unknown"})
	}
	return out
}

type PublicStatus struct {
	ProxyEnabled  bool         `json:"proxy_enabled"`
	Configured    bool         `json:"configured"`
	Route         RouteState   `json:"route"`
	Nodes         int          `json:"nodes"`
	Skipped       int          `json:"skipped"`
	CacheAge      string       `json:"cache_age,omitempty"`
	Stale         bool         `json:"stale"`
	Expired       bool         `json:"expired"`
	LastRefresh   string       `json:"last_refresh,omitempty"`
	ErrorCode     string       `json:"error_code,omitempty"`
	NodeStatuses  []NodeStatus `json:"node_statuses,omitempty"`
	Managed       bool         `json:"managed,omitempty"`
	Mode          string       `json:"mode,omitempty"`
	PolicyVersion int          `json:"policy_version,omitempty"`
}

type Target struct {
	Host string
	Port int
}

func (t Target) Addr() string {
	return net.JoinHostPort(t.Host, fmt.Sprintf("%d", t.Port))
}

func CanonicalHostPort(host string, port int) (string, int, error) {
	return snellspec.CanonicalHostPort(host, port)
}

func SplitHostPort(addr string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return "", 0, errCode("invalid_target")
	}
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		return "", 0, errCode("invalid_target")
	}
	return CanonicalHostPort(host, port)
}
