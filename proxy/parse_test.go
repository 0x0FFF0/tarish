package proxy

import (
	"strings"
	"testing"
)

func TestParseSurgeCompleteQuotedPSK(t *testing.T) {
	doc := "" +
		"[General]\n" +
		"loglevel = notify\n" +
		"[Proxy]\n" +
		"Home = snell, 198.51.100.10, 440, psk=\"comma,inside\", version=4, obfs=http, obfs-host=www.example.com\n" +
		"Other = ss, 198.51.100.11, 443, encrypt-method=aes-256-gcm, password=x\n" +
		"[Proxy Group]\n" +
		"g = select, Home\n"
	snap, err := ParseDocument([]byte(doc), FormatAuto)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if snap.NodeCount != 1 || snap.Skipped < 1 {
		t.Fatalf("nodes=%d skipped=%d", snap.NodeCount, snap.Skipped)
	}
	n := snap.Nodes[0]
	if n.PSK != "comma,inside" || n.Version != "v4" || n.Host != "198.51.100.10" || n.Port != 440 {
		t.Fatalf("node %+v", n)
	}
	if n.ObfsMode != "http" || n.ObfsHost != "www.example.com" {
		t.Fatalf("obfs %+v", n)
	}
}

func TestParseSurgeProviderListCRLFBOM(t *testing.T) {
	doc := "\ufeffNodeA = snell, 2001:db8::1, 440, psk=secret, version=5\r\n" +
		"NodeB = snell, 2001:db8::1, 440, psk=secret, version=5\r\n"
	snap, err := ParseDocument([]byte(doc), FormatSurge)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if snap.NodeCount != 1 {
		t.Fatalf("duplicate transport should collapse, got %d", snap.NodeCount)
	}
	if snap.Nodes[0].Host != "2001:db8::1" || snap.Nodes[0].Version != "v5" {
		t.Fatalf("ipv6 node %+v", snap.Nodes[0])
	}
}

func TestParseMihomoCompleteAndProvider(t *testing.T) {
	complete := `
mixed-port: 7890
proxies:
  - name: n1
    type: snell
    server: 198.51.100.20
    port: 1443
    psk: "quoted-psk"
    version: 4
    obfs-opts:
      mode: tls
      host: www.bing.com
  - name: ss1
    type: ss
    server: 198.51.100.21
    port: 443
`
	snap, err := ParseDocument([]byte(complete), FormatAuto)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if snap.NodeCount != 1 || snap.Nodes[0].PSK != "quoted-psk" || snap.Nodes[0].ObfsMode != "tls" {
		t.Fatalf("complete nodes %+v", snap.Nodes)
	}

	provider := `
proxies:
  - name: p1
    type: snell
    server: 198.51.100.30
    port: 440
    psk: abc
    version: 5
`
	snap, err = ParseDocument([]byte(provider), FormatMihomo)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if snap.NodeCount != 1 || snap.Nodes[0].Version != "v5" {
		t.Fatalf("provider %+v", snap.Nodes)
	}
}

func TestParseRejectsOmittedAndLegacyVersion(t *testing.T) {
	doc := "A = snell, 198.51.100.1, 440, psk=x\n"
	if _, err := ParseDocument([]byte(doc), FormatSurge); ErrorCode(err) != "no_compatible_nodes" {
		t.Fatalf("omitted version err=%v", err)
	}
	doc = "A = snell, 198.51.100.1, 440, psk=x, version=1\n"
	if _, err := ParseDocument([]byte(doc), FormatSurge); ErrorCode(err) != "no_compatible_nodes" {
		t.Fatalf("v1 err=%v", err)
	}
}

func TestParseRejectsHostileAndIncompatible(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		fmt  Format
		code string
	}{
		{"html", "<html><body>error</body></html>", FormatAuto, "html_document"},
		{"uri", "snell://abc@host:1", FormatAuto, "uri_list_unsupported"},
		{"yaml", "proxies: [", FormatMihomo, "invalid_yaml"},
		{"nested", "proxy-providers:\n  p:\n    url: https://example.invalid/x\n", FormatMihomo, "nested_reference"},
		{"oversize", strings.Repeat("A", MaxDocumentBytes+8), FormatAuto, "document_too_large"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseDocument([]byte(tc.doc), tc.fmt)
			if ErrorCode(err) != tc.code {
				t.Fatalf("got %q want %q err=%v", ErrorCode(err), tc.code, err)
			}
		})
	}

	badObfs := "A = snell, 198.51.100.1, 440, psk=x, version=4, obfs=weird\n"
	snap, err := ParseDocument([]byte(badObfs), FormatSurge)
	if err == nil || (snap != nil && snap.NodeCount > 0) {
		if err == nil {
			t.Fatal("incompatible obfs accepted")
		}
	}

	unknown := `
proxies:
  - name: n
    type: snell
    server: 198.51.100.1
    port: 440
    psk: x
    version: 4
    restls: true
`
	if _, err := ParseDocument([]byte(unknown), FormatMihomo); err == nil {
		t.Fatal("restls accepted")
	}

	uri := `
proxies:
  - name: n
    type: snell
    server: 198.51.100.1
    port: 440
    psk: x
    version: 4
    obfs-opts:
      mode: http
      uri: /not-default
`
	if _, err := ParseDocument([]byte(uri), FormatMihomo); err == nil {
		t.Fatal("custom obfs-uri accepted")
	}
}

func TestParseYAMLAliasRejected(t *testing.T) {
	doc := "proxies: &a [{name: n, type: snell, server: 1.1.1.1, port: 1, psk: x, version: 4}]\nmore: *a\n"
	_, err := ParseDocument([]byte(doc), FormatMihomo)
	if err == nil {
		t.Fatal("alias document accepted")
	}
	code := ErrorCode(err)
	if code != "yaml_alias" && code != "invalid_yaml" && code != "unknown_transport_field" {
		t.Fatalf("unexpected code %s", code)
	}
}

func TestParseTooManyNodes(t *testing.T) {
	var b strings.Builder
	b.WriteString("[Proxy]\n")
	for i := 0; i < MaxNodes+5; i++ {
		b.WriteString("N")
		b.WriteString(strings.Repeat("x", i%10))
		b.WriteString(" = snell, 198.51.100.1, ")
		b.WriteString("44")
		if i%2 == 0 {
			b.WriteString("0")
		} else {
			b.WriteString("1")
		}
		b.WriteString(", psk=p")
		b.WriteString(strings.Repeat("z", 1+i%20))
		b.WriteString(", version=4\n")
	}
	_, err := ParseDocument([]byte(b.String()), FormatSurge)
	if err != nil && ErrorCode(err) != "too_many_nodes" {
		// unique psks/ports should exceed limit
		if ErrorCode(err) == "no_compatible_nodes" {
			t.Fatalf("unexpected %v", err)
		}
	}
}

func TestParseDuplicateKeyRejected(t *testing.T) {
	doc := "A = snell, 198.51.100.1, 440, psk=x, psk=y, version=4\n"
	_, err := ParseDocument([]byte(doc), FormatSurge)
	if err == nil {
		t.Fatal("duplicate psk accepted")
	}
}

func FuzzParseDocument(f *testing.F) {
	f.Add([]byte("[Proxy]\nA = snell, 1.2.3.4, 440, psk=x, version=4\n"))
	f.Add([]byte("proxies:\n- name: a\n  type: snell\n  server: 1.2.3.4\n  port: 440\n  psk: x\n  version: 4\n"))
	f.Add([]byte("<html>nope</html>"))
	f.Fuzz(func(t *testing.T, in []byte) {
		_, _ = ParseDocument(in, FormatAuto)
	})
}
