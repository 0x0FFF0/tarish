package proxy

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"tarish/snellspec"
)

// liveShapeDoc matches the operator panel: provider-list (no [Proxy] section),
// emoji names, spaces around psk/version '=', Snell v5, and one seed-equal transport.
func liveShapeDoc() string {
	seed := BootstrapNode()
	return strings.Join([]string{
		"🇺🇸 OWOUS = snell, 23.191.8.37, 63809, psk = synth-owo, version = 5",
		"🇺🇸 US 9929 = snell, 69.63.207.145, 60138, psk = synth-9929, version = 5",
		"🇯🇵 JP IIJ = snell, 193.111.31.105, 63597, psk = synth-jp, version = 5",
		"🇦🇺 AU GC 9929 = snell, 193.177.221.84, 62719, psk = synth-au, version = 5",
		"🇭🇰 HK HTBT = snell, 156.238.121.162, 49173, psk = synth-hk, version = 5",
		"🇺🇸 US SJC 11TB = snell, 173.249.207.156, 62763, psk = synth-sjc, version = 5",
		"🇺🇸 US CN2 2T B = snell, " + seed.Host + ", " + strconv.Itoa(seed.Port) + ", psk = " + seed.PSK + ", version = 5",
		"🇺🇸 US CN2 2T A = snell, 144.34.232.133, 62059, psk = synth-cn2a, version = 5",
		"🇺🇸 NAS = snell, 188.209.141.172, 24000, psk = synth-nas, version = 5",
	}, "\n") + "\n"
}

func TestParseLiveSurgeShape(t *testing.T) {
	doc := liveShapeDoc()
	if strings.Contains(strings.ToLower(doc), "[proxy]") {
		t.Fatal("fixture must be a provider list")
	}
	snap, err := ParseDocument([]byte(doc), FormatSurge)
	if err != nil {
		t.Fatalf("parse live shape: %v", err)
	}
	if snap.NodeCount != 9 {
		t.Fatalf("nodes=%d want 9", snap.NodeCount)
	}
	v5 := 0
	builtin := 0
	byName := map[string]NodeSpec{}
	for _, n := range snap.Nodes {
		if n.Version != "v5" {
			t.Fatalf("expected v5, got %+v", n)
		}
		v5++
		if isBootstrap(n) {
			builtin++
			if n.Role() != snellspec.RoleBuiltin {
				t.Fatalf("seed role %q", n.Role())
			}
			if n.ID != BootstrapNode().ID {
				t.Fatalf("seed id %s", n.ID)
			}
		}
		byName[n.Name] = n
	}
	if v5 != 9 || builtin != 1 {
		t.Fatalf("v5=%d builtin=%d", v5, builtin)
	}
	if _, ok := byName["🇺🇸 OWOUS"]; !ok {
		t.Fatalf("emoji name missing: %v", namesOf(snap))
	}

	again, err := ParseDocument([]byte(doc), FormatSurge)
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	assertSameIDs(t, snap, again)

	renamed := strings.ReplaceAll(doc, "🇺🇸 OWOUS", "renamed-owo")
	renamedSnap, err := ParseDocument([]byte(renamed), FormatSurge)
	if err != nil {
		t.Fatalf("rename parse: %v", err)
	}
	assertSameIDs(t, snap, renamedSnap)

	rotated := strings.ReplaceAll(doc, "psk = synth-owo", "psk = synth-owo-rotated")
	rotatedSnap, err := ParseDocument([]byte(rotated), FormatSurge)
	if err != nil {
		t.Fatalf("rotate parse: %v", err)
	}
	oldID := byName["🇺🇸 OWOUS"].ID
	newID := ""
	for _, n := range rotatedSnap.Nodes {
		if n.Name == "🇺🇸 OWOUS" {
			newID = n.ID
		}
	}
	if newID == "" || newID == oldID {
		t.Fatalf("credential change reused id %s", oldID)
	}

	summary := snellspec.RedactedSummary(snap)
	if strings.Contains(summary, "synth-owo") || strings.Contains(strings.ToLower(summary), "psk") {
		t.Fatalf("summary leaked secret: %s", summary)
	}
	if !strings.Contains(summary, "builtin_fallback=1") {
		t.Fatalf("summary missing seed mark: %s", summary)
	}
	if path := os.Getenv("TARISH_SNELL_PARSE_LOG"); path != "" {
		if err := os.WriteFile(path, []byte(summary+"\n"), 0o600); err != nil {
			t.Fatalf("write parse log: %v", err)
		}
	}
}

func TestParseCapturedLiveBody(t *testing.T) {
	path := os.Getenv("TARISH_SNELL_LIVE_BODY")
	if path == "" {
		t.Skip("TARISH_SNELL_LIVE_BODY not set")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read captured body: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("captured body empty")
	}
	snap, err := ParseDocument(raw, FormatSurge)
	if err != nil {
		t.Fatalf("parse captured: %v", err)
	}
	if snap.NodeCount < 1 {
		t.Fatal("captured parse produced no nodes")
	}
	v5 := 0
	builtin := 0
	for _, n := range snap.Nodes {
		if n.Version == "v5" {
			v5++
		}
		if isBootstrap(n) {
			builtin++
		}
	}
	if v5 == 0 {
		t.Fatalf("expected snell v5 nodes, got %+v", versionsOf(snap))
	}
	if builtin != 1 {
		t.Fatalf("seed-equal transport not marked 内置兜底, builtin=%d nodes=%d", builtin, snap.NodeCount)
	}
	again, err := ParseDocument(raw, FormatSurge)
	if err != nil {
		t.Fatalf("reparse captured: %v", err)
	}
	assertSameIDs(t, snap, again)
	summary := snellspec.RedactedSummary(snap)
	if strings.Contains(strings.ToLower(summary), "psk") || strings.Contains(summary, "token=") {
		t.Fatalf("captured summary leaked secret")
	}
	if path := os.Getenv("TARISH_SNELL_PARSE_LOG"); path != "" {
		if err := os.WriteFile(path, []byte(summary+"\n"), 0o600); err != nil {
			t.Fatalf("write parse log: %v", err)
		}
	}
}

func assertSameIDs(t *testing.T, a, b *Snapshot) {
	t.Helper()
	if a.NodeCount != b.NodeCount {
		t.Fatalf("count %d vs %d", a.NodeCount, b.NodeCount)
	}
	am := map[string]string{}
	for _, n := range a.Nodes {
		am[n.TransportKey()] = n.ID
	}
	for _, n := range b.Nodes {
		if am[n.TransportKey()] != n.ID {
			t.Fatalf("id changed for %s: %s vs %s", n.Name, am[n.TransportKey()], n.ID)
		}
	}
}

func namesOf(s *Snapshot) []string {
	out := make([]string, 0, len(s.Nodes))
	for _, n := range s.Nodes {
		out = append(out, n.Name)
	}
	return out
}

func versionsOf(s *Snapshot) []string {
	out := make([]string, 0, len(s.Nodes))
	for _, n := range s.Nodes {
		out = append(out, n.Version)
	}
	return out
}
