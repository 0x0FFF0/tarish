package snellspec

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type ParseResult struct {
	Snapshot    *Snapshot
	Diagnostics []Diagnostic
	Skipped     int
}

var surgeKnownKeys = map[string]string{
	"psk": "psk", "password": "psk",
	"version": "version", "ver": "version",
	"obfs": "obfs", "obfs-host": "obfs-host", "obfs-uri": "obfs-uri",
	"tfo": "tfo", "udp": "udp", "reuse": "reuse",
	"ip-version": "ip-version",
}

var surgeIgnoreKeys = map[string]bool{
	"test-url": true, "test-timeout": true, "backend": true,
}

var mihomoKnown = map[string]bool{
	"name": true, "type": true, "server": true, "port": true, "psk": true,
	"version": true, "obfs-opts": true, "udp": true, "tfo": true, "reuse": true,
	"ip-version": true,
}

var mihomoDisplay = map[string]bool{
	"icon": true, "extra": true,
}

func ParseDocument(raw []byte, format Format) (*Snapshot, error) {
	if len(raw) > MaxDocumentBytes {
		return nil, Err("document_too_large")
	}
	raw = normalizeDoc(raw)
	if looksHTML(raw) {
		return nil, Err("html_document")
	}
	if looksURIList(raw) {
		return nil, Err("uri_list_unsupported")
	}

	detected, err := detectFormat(raw, format)
	if err != nil {
		return nil, err
	}

	var nodes []NodeSpec
	var skipped int
	var diags []Diagnostic

	switch detected {
	case FormatSurge:
		nodes, skipped, diags, err = parseSurge(raw)
	case FormatMihomo:
		nodes, skipped, diags, err = parseMihomo(raw)
	default:
		return nil, Err("ambiguous_format")
	}
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, Err("no_compatible_nodes")
	}
	if len(nodes) > MaxNodes {
		return nil, Err("too_many_nodes")
	}

	seen := map[string]string{}
	out := make([]NodeSpec, 0, len(nodes))
	for _, n := range nodes {
		n = FinalizeNode(n)
		k := n.TransportKey()
		if prev, ok := seen[k]; ok {
			skipped++
			diags = append(diags, Diagnostic{Code: "duplicate_node"})
			_ = prev
			continue
		}
		seen[k] = n.ID
		out = append(out, n)
	}

	return &Snapshot{
		Nodes:       out,
		Skipped:     skipped,
		Diagnostics: diags,
		Format:      detected,
		NodeCount:   len(out),
	}, nil
}

func normalizeDoc(raw []byte) []byte {
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	raw = bytes.ReplaceAll(raw, []byte("\r"), []byte("\n"))
	return raw
}

func looksHTML(raw []byte) bool {
	s := strings.TrimSpace(string(raw))
	ls := strings.ToLower(s)
	return strings.HasPrefix(ls, "<!doctype html") || strings.HasPrefix(ls, "<html") ||
		strings.Contains(ls, "<html") && strings.Contains(ls, "<body")
}

func looksURIList(raw []byte) bool {
	s := strings.TrimSpace(string(raw))
	ls := strings.ToLower(s)
	if strings.HasPrefix(ls, "snell://") || strings.Contains(ls, "\nsnell://") {
		return true
	}
	// A single long base64 blob with no proxy markers.
	if !strings.Contains(ls, "[proxy]") && !strings.Contains(ls, "proxies:") &&
		!strings.Contains(ls, " = snell") && !strings.Contains(ls, "type: snell") {
		trimmed := strings.ReplaceAll(strings.ReplaceAll(s, "\n", ""), " ", "")
		if len(trimmed) > 80 && isMostlyBase64(trimmed) {
			return true
		}
	}
	return false
}

func isMostlyBase64(s string) bool {
	n := 0
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '+' || r == '/' || r == '=' {
			n++
		}
	}
	return n*10 >= len(s)*9
}

func detectFormat(raw []byte, format Format) (Format, error) {
	s := strings.TrimSpace(string(raw))
	ls := strings.ToLower(s)
	hasSurge := strings.Contains(ls, "[proxy]") || surgeLinePresent(s)
	hasMihomo := looksMihomo(ls)

	switch format {
	case FormatSurge:
		if !hasSurge && hasMihomo {
			return "", Err("format_mismatch")
		}
		return FormatSurge, nil
	case FormatMihomo:
		if !hasMihomo && hasSurge {
			return "", Err("format_mismatch")
		}
		return FormatMihomo, nil
	case FormatAuto, "":
		if hasSurge && hasMihomo {
			return "", Err("ambiguous_format")
		}
		if hasSurge {
			return FormatSurge, nil
		}
		if hasMihomo {
			return FormatMihomo, nil
		}
		return "", Err("unrecognized_document")
	default:
		return "", Err("unknown_format")
	}
}

func surgeLinePresent(s string) bool {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		low := strings.ToLower(line)
		if strings.Contains(low, "= snell,") || strings.Contains(low, "=snell,") {
			return true
		}
	}
	return false
}

func looksMihomo(ls string) bool {
	return strings.Contains(ls, "proxies:") || strings.Contains(ls, "\nproxies:") ||
		strings.HasPrefix(ls, "proxies:") || strings.Contains(ls, "type: snell")
}

func parseSurge(raw []byte) ([]NodeSpec, int, []Diagnostic, error) {
	text := string(raw)
	section := extractSurgeProxySection(text)
	skipped := 0
	var diags []Diagnostic
	var nodes []NodeSpec
	for _, line := range strings.Split(section, "\n") {
		orig := line
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "[") {
			continue
		}
		name, rest, ok := splitSurgeName(line)
		if !ok {
			skipped++
			continue
		}
		fields, err := splitCSV(rest)
		if err != nil {
			skipped++
			diags = append(diags, Diagnostic{Code: "invalid_line"})
			continue
		}
		if len(fields) < 3 {
			skipped++
			diags = append(diags, Diagnostic{Code: "invalid_line"})
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(fields[0]))
		if typ != "snell" {
			skipped++
			continue
		}
		n, derr, skipCode := surgeNode(name, fields[1:])
		if skipCode != "" {
			skipped++
			diags = append(diags, Diagnostic{Code: skipCode})
			_ = orig
			_ = derr
			continue
		}
		nodes = append(nodes, n)
	}
	return nodes, skipped, diags, nil
}

func extractSurgeProxySection(text string) string {
	lines := strings.Split(text, "\n")
	var b strings.Builder
	inProxy := false
	sawSection := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		low := strings.ToLower(trim)
		if strings.HasPrefix(low, "[") && strings.HasSuffix(low, "]") {
			inProxy = low == "[proxy]"
			if inProxy {
				sawSection = true
			}
			continue
		}
		if inProxy {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	if sawSection {
		return b.String()
	}
	// Provider/list: whole document is name = snell, ...
	return text
}

func splitSurgeName(line string) (name, rest string, ok bool) {
	eq := -1
	for i, r := range line {
		if r == '=' {
			eq = i
			break
		}
	}
	if eq <= 0 {
		return "", "", false
	}
	name = strings.TrimSpace(line[:eq])
	rest = strings.TrimSpace(line[eq+1:])
	if name == "" || rest == "" {
		return "", "", false
	}
	return name, rest, true
}

func surgeNode(name string, fields []string) (NodeSpec, error, string) {
	host := strings.TrimSpace(fields[0])
	port, err := strconv.Atoi(strings.TrimSpace(fields[1]))
	if err != nil {
		return NodeSpec{}, err, "invalid_port"
	}
	host, port, err = CanonicalHostPort(host, port)
	if err != nil {
		return NodeSpec{}, err, "invalid_target"
	}
	kv := map[string]string{}
	for _, f := range fields[2:] {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		k, v, ok := strings.Cut(f, "=")
		if !ok {
			return NodeSpec{}, Err("invalid_line"), "invalid_line"
		}
		key := strings.ToLower(strings.TrimSpace(k))
		val := strings.TrimSpace(v)
		if mapped, known := surgeKnownKeys[key]; known {
			if _, exists := kv[mapped]; exists {
				return NodeSpec{}, Err("duplicate_key"), "duplicate_key"
			}
			kv[mapped] = val
			continue
		}
		if surgeIgnoreKeys[key] {
			continue
		}
		return NodeSpec{}, Err("unknown_transport_field"), "unknown_transport_field"
	}
	psk := kv["psk"]
	if psk == "" {
		return NodeSpec{}, Err("missing_psk"), "missing_psk"
	}
	ver, code := normalizeVersion(kv["version"])
	if code != "" {
		return NodeSpec{}, Err(code), code
	}
	obfs := strings.ToLower(kv["obfs"])
	switch obfs {
	case "", "off", "none", "http", "tls":
		if obfs == "none" {
			obfs = "off"
		}
	default:
		return NodeSpec{}, Err("incompatible_obfs"), "incompatible_obfs"
	}
	if uri := kv["obfs-uri"]; uri != "" && uri != "/" {
		return NodeSpec{}, Err("unsupported_obfs_uri"), "unsupported_obfs_uri"
	}
	_ = kv["tfo"]
	_ = kv["udp"]
	_ = kv["reuse"]
	return NodeSpec{
		Name:     name,
		Host:     host,
		Port:     port,
		PSK:      psk,
		Version:  ver,
		ObfsMode: obfs,
		ObfsHost: kv["obfs-host"],
	}, nil, ""
}

func normalizeVersion(v string) (string, string) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", "legacy_version"
	}
	switch strings.ToLower(v) {
	case "4", "v4":
		return "v4", ""
	case "5", "v5":
		return "v5", ""
	case "1", "v1", "2", "v2", "3", "v3":
		return "", "legacy_version"
	case "6", "v6":
		return "", "unsupported_version"
	default:
		return "", "unsupported_version"
	}
}

func splitCSV(s string) ([]string, error) {
	var parts []string
	var buf strings.Builder
	var quote rune
	esc := false
	for _, r := range s {
		if esc {
			buf.WriteRune(r)
			esc = false
			continue
		}
		if r == '\\' && quote != 0 {
			esc = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				buf.WriteRune(r)
			}
			continue
		}
		if r == '"' || r == '\'' {
			quote = r
			continue
		}
		if r == ',' {
			parts = append(parts, strings.TrimSpace(buf.String()))
			buf.Reset()
			continue
		}
		buf.WriteRune(r)
	}
	if quote != 0 {
		return nil, Err("unclosed_quote")
	}
	parts = append(parts, strings.TrimSpace(buf.String()))
	return parts, nil
}

func parseMihomo(raw []byte) ([]NodeSpec, int, []Diagnostic, error) {
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	var root yaml.Node
	if err := dec.Decode(&root); err != nil {
		return nil, 0, nil, Err("invalid_yaml")
	}
	if err := inspectYAML(&root, 0); err != nil {
		return nil, 0, nil, err
	}
	var doc any
	if err := root.Decode(&doc); err != nil {
		return nil, 0, nil, Err("invalid_yaml")
	}
	proxies, err := extractProxies(doc)
	if err != nil {
		return nil, 0, nil, err
	}
	skipped := 0
	var diags []Diagnostic
	var nodes []NodeSpec
	for _, p := range proxies {
		m, ok := p.(map[string]any)
		if !ok {
			skipped++
			diags = append(diags, Diagnostic{Code: "invalid_node"})
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(fmt.Sprint(nilStr(m["type"]))))
		if typ != "snell" {
			skipped++
			continue
		}
		n, code := mihomoNode(m)
		if code != "" {
			skipped++
			diags = append(diags, Diagnostic{Code: code})
			continue
		}
		nodes = append(nodes, n)
	}
	return nodes, skipped, diags, nil
}

func inspectYAML(n *yaml.Node, depth int) error {
	if n == nil {
		return nil
	}
	if depth > MaxYAMLDepth {
		return Err("yaml_too_deep")
	}
	if n.Kind == yaml.AliasNode {
		return Err("yaml_alias")
	}
	for _, c := range n.Content {
		if err := inspectYAML(c, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func extractProxies(doc any) ([]any, error) {
	switch v := doc.(type) {
	case map[string]any:
		if p, ok := v["proxies"]; ok {
			list, ok := p.([]any)
			if !ok {
				return nil, Err("invalid_yaml")
			}
			return list, nil
		}
		if _, has := v["proxy-providers"]; has {
			return nil, Err("nested_reference")
		}
		if _, has := v["include"]; has {
			return nil, Err("nested_reference")
		}
		return nil, Err("unrecognized_document")
	case []any:
		return v, nil
	default:
		return nil, Err("unrecognized_document")
	}
}

func nestedRef(m map[string]any) bool {
	for _, k := range []string{"proxy-providers", "rule-providers", "include", "proxy-groups"} {
		if _, ok := m[k]; ok {
			// groups/rules are allowed in complete profiles; we do not fetch them.
			if k == "proxy-providers" || k == "rule-providers" || k == "include" {
				return true
			}
		}
	}
	return false
}

func mihomoNode(m map[string]any) (NodeSpec, string) {
	for k := range m {
		lk := strings.ToLower(k)
		if mihomoKnown[lk] || mihomoDisplay[lk] {
			continue
		}
		if lk == "shadow-tls" || lk == "restls" || lk == "jls" || lk == "client-fingerprint" ||
			lk == "dialer-proxy" || lk == "smux" || lk == "plugin" || lk == "plugin-opts" {
			return NodeSpec{}, "incompatible_transport"
		}
		return NodeSpec{}, "unknown_transport_field"
	}
	host := strings.TrimSpace(fmt.Sprint(m["server"]))
	port := anyInt(m["port"])
	host, port, err := CanonicalHostPort(host, port)
	if err != nil {
		return NodeSpec{}, "invalid_target"
	}
	psk := strings.TrimSpace(fmt.Sprint(nilStr(m["psk"])))
	if psk == "" {
		return NodeSpec{}, "missing_psk"
	}
	verRaw := ""
	if v, ok := m["version"]; ok && v != nil {
		verRaw = fmt.Sprint(v)
	}
	ver, code := normalizeVersion(verRaw)
	if code != "" {
		return NodeSpec{}, code
	}
	obfsMode, obfsHost, obfsURI, code := mihomoObfs(m["obfs-opts"])
	if code != "" {
		return NodeSpec{}, code
	}
	_ = m["udp"]
	_ = m["tfo"]
	_ = m["reuse"]
	name := strings.TrimSpace(fmt.Sprint(nilStr(m["name"])))
	_ = obfsURI
	return NodeSpec{
		Name:     name,
		Host:     host,
		Port:     port,
		PSK:      psk,
		Version:  ver,
		ObfsMode: obfsMode,
		ObfsHost: obfsHost,
	}, ""
}

func mihomoObfs(v any) (mode, host, uri, code string) {
	if v == nil {
		return "", "", "", ""
	}
	m, ok := v.(map[string]any)
	if !ok {
		return "", "", "", "incompatible_obfs"
	}
	for k := range m {
		lk := strings.ToLower(k)
		if lk != "mode" && lk != "host" && lk != "uri" && lk != "obfs-uri" {
			return "", "", "", "unknown_transport_field"
		}
	}
	mode = strings.ToLower(strings.TrimSpace(fmt.Sprint(nilStr(m["mode"]))))
	switch mode {
	case "", "off", "http", "tls":
	default:
		return "", "", "", "incompatible_obfs"
	}
	host = strings.TrimSpace(fmt.Sprint(nilStr(m["host"])))
	uri = strings.TrimSpace(fmt.Sprint(nilStr(m["uri"])))
	if uri == "" {
		uri = strings.TrimSpace(fmt.Sprint(nilStr(m["obfs-uri"])))
	}
	if uri != "" && uri != "/" {
		return "", "", "", "unsupported_obfs_uri"
	}
	return mode, host, uri, ""
}

func nilStr(v any) any {
	if v == nil {
		return ""
	}
	return v
}

func anyInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case uint64:
		return int(t)
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	default:
		n, _ := strconv.Atoi(strings.TrimSpace(fmt.Sprint(v)))
		return n
	}
}

func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return FormatAuto, nil
	case "surge":
		return FormatSurge, nil
	case "mihomo", "clash", "clash-meta":
		return FormatMihomo, nil
	default:
		return "", Err("unknown_format")
	}
}
