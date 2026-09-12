package snellspec

import (
	"fmt"
	"net"
	"strings"
	"time"
)

const (
	MaxDocumentBytes = 4 << 20
	MaxNodes         = 1000
	MaxYAMLDepth     = 20
	ProtocolVersion  = 1
	StaleAfter       = 24 * time.Hour
	MaxCacheAge      = 7 * 24 * time.Hour
	RoleBuiltin      = "内置兜底"
)

type Format string

const (
	FormatAuto   Format = "auto"
	FormatSurge  Format = "surge"
	FormatMihomo Format = "mihomo"
)

// NodeSpec is the transport description of a Snell node.
// It must not be logged verbatim; PSK and host are secrets.
type NodeSpec struct {
	ID       string
	Name     string
	Host     string
	Port     int
	PSK      string
	Version  string // "v4" or "v5"
	ObfsMode string
	ObfsHost string
	Builtin  bool
}

func (n NodeSpec) ServerAddr() string {
	return net.JoinHostPort(n.Host, fmt.Sprintf("%d", n.Port))
}

func (n NodeSpec) TransportKey() string {
	return strings.Join([]string{
		strings.ToLower(n.Host),
		fmt.Sprintf("%d", n.Port),
		n.PSK,
		n.Version,
		n.ObfsMode,
		n.ObfsHost,
	}, "\x00")
}

func (n NodeSpec) TransportEqual(o NodeSpec) bool {
	return n.TransportKey() == o.TransportKey()
}

func (n NodeSpec) Role() string {
	if n.Builtin || IsSeed(n) {
		return RoleBuiltin
	}
	return ""
}

type Diagnostic struct {
	Code    string `json:"code"`
	Skipped int    `json:"skipped,omitempty"`
}

type Snapshot struct {
	Nodes        []NodeSpec
	Skipped      int
	Diagnostics  []Diagnostic
	FetchedAt    time.Time
	ValidatedAt  time.Time
	ETag         string
	LastModified string
	Format       Format
	NodeCount    int
}

func (s *Snapshot) Clone() *Snapshot {
	if s == nil {
		return nil
	}
	cp := *s
	cp.Nodes = append([]NodeSpec(nil), s.Nodes...)
	cp.Diagnostics = append([]Diagnostic(nil), s.Diagnostics...)
	return &cp
}

func CanonicalHostPort(host string, port int) (string, int, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", 0, Err("invalid_target")
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	if ip := net.ParseIP(host); ip != nil {
		host = ip.String()
	} else {
		host = strings.ToLower(host)
	}
	if port < 1 || port > 65535 {
		return "", 0, Err("invalid_target")
	}
	return host, port, nil
}

func ValidHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	return host != "" && !strings.ContainsAny(host, " /")
}

func CacheFreshness(validated time.Time, now time.Time) (stale, expired bool, age time.Duration) {
	if validated.IsZero() {
		return true, true, 0
	}
	age = now.Sub(validated)
	if age < 0 {
		age = 0
	}
	expired = age > MaxCacheAge
	stale = age > StaleAfter
	return stale, expired, age
}
