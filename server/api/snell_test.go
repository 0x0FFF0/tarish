package api

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tarish-server/models"
	"tarish-server/snell"
	"tarish-server/store"
	"tarish/proxy"
	"tarish/snellspec"
)

func testAPI(t *testing.T, agentKey string) (*Server, *httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "tarish.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv := NewServer(st, nil, agentKey, nil)
	hs := httptest.NewServer(srv.Routes())
	t.Cleanup(hs.Close)
	return srv, hs, st
}

func surgeBody() string {
	return "🇺🇸 NodeA = snell, 198.51.100.10, 440, psk = secret-a, version = 5\n" +
		"🇺🇸 NodeB = snell, 198.51.100.11, 441, psk = secret-b, version = 5\n"
}

func testUpstream(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(handler)
}

func loopbackFetcher(upstream *httptest.Server) *snell.Fetcher {
	f := snell.NewFetcher(snell.Limits{
		AllowLoopback: true,
		TLSConfig:     &tls.Config{InsecureSkipVerify: true},
		Timeout:       5 * time.Second,
	})
	return f
}

func TestSnellSourceSyncAndKeepLastGood(t *testing.T) {
	var body = surgeBody()
	var mode = "ok"
	up := testUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case "html":
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, "<html>nope</html>")
		case "empty":
			io.WriteString(w, "Other = ss, 1.1.1.1, 443, password=x\n")
		default:
			io.WriteString(w, body)
		}
	})
	defer up.Close()

	apiSrv, hs, st := testAPI(t, "k")
	apiSrv.SetSnellFetcher(loopbackFetcher(up))

	secretURL := up.URL + "/api/subscribe?token=VerySecret&format=surge"
	add, _ := json.Marshal(map[string]string{"url": secretURL, "format": "surge", "name": "panel"})
	resp, err := http.Post(hs.URL+"/api/snell/sources", "application/json", bytes.NewReader(add))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("add status %d", resp.StatusCode)
	}
	var src models.SnellSource
	if err := json.NewDecoder(resp.Body).Decode(&src); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(src.URL, "VerySecret") || strings.Contains(src.URL, "token=") || src.URL == secretURL {
		t.Fatalf("url not redacted: %s", src.URL)
	}
	if src.LastError != "" {
		t.Fatalf("initial sync error %s", src.LastError)
	}

	nodes, _ := st.ListNodeViews()
	if len(nodes) < 2 {
		t.Fatalf("nodes %d", len(nodes))
	}
	firstCount := len(nodes)

	mode = "html"
	resp, err = http.Post(hs.URL+"/api/snell/sources/"+src.ID+"/sync", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	nodes, _ = st.ListNodeViews()
	if len(nodes) != firstCount {
		t.Fatalf("html clobbered catalog %d -> %d", firstCount, len(nodes))
	}
	got, _ := st.GetSource(src.ID)
	if got.LastError != "html_document" {
		t.Fatalf("html error %s", got.LastError)
	}

	mode = "empty"
	resp, err = http.Post(hs.URL+"/api/snell/sources/"+src.ID+"/sync", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	nodes, _ = st.ListNodeViews()
	if len(nodes) != firstCount {
		t.Fatalf("empty clobbered catalog")
	}
}

func TestSnellDefaultPoolIncludesNewUpstreamNode(t *testing.T) {
	var extra bool
	up := testUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, surgeBody())
		if extra {
			io.WriteString(w, "🇺🇸 NodeC = snell, 198.51.100.12, 442, psk = secret-c, version = 5\n")
		}
	})
	defer up.Close()
	apiSrv, hs, st := testAPI(t, "k")
	apiSrv.SetSnellFetcher(loopbackFetcher(up))

	add, _ := json.Marshal(map[string]string{"url": up.URL + "/sub?token=VerySecret", "format": "surge"})
	resp, _ := http.Post(hs.URL+"/api/snell/sources", "application/json", bytes.NewReader(add))
	var src models.SnellSource
	json.NewDecoder(resp.Body).Decode(&src)
	resp.Body.Close()

	poolBody, _ := json.Marshal(map[string]any{"include_source_ids": []string{src.ID}, "managed_proxy_enabled": true})
	req, _ := http.NewRequest(http.MethodPut, hs.URL+"/api/snell/pool", bytes.NewReader(poolBody))
	req.Header.Set("Content-Type", "application/json")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()

	if err := st.UpsertMiner(&models.AgentReport{MinerID: "n", WorkerID: "w1", Hostname: "h", Capabilities: []string{"snell-managed"}}); err != nil {
		t.Fatal(err)
	}
	before, _ := st.EffectiveSnapshot("w1")
	if len(before.Nodes) != 2 {
		t.Fatalf("before nodes %d", len(before.Nodes))
	}
	extra = true
	resp, _ = http.Post(hs.URL+"/api/snell/sources/"+src.ID+"/sync", "application/json", nil)
	resp.Body.Close()
	after, _ := st.EffectiveSnapshot("w1")
	if len(after.Nodes) != 3 {
		t.Fatalf("new upstream node not in default pool: %d", len(after.Nodes))
	}
}

func TestSnellPendingAuthAndRedaction(t *testing.T) {
	up := testUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, surgeBody())
	})
	defer up.Close()
	apiSrv, hs, st := testAPI(t, "secret-key")
	apiSrv.SetSnellFetcher(loopbackFetcher(up))
	secretURL := up.URL + "/api/subscribe?token=VerySecret&format=surge"
	add, _ := json.Marshal(map[string]string{"url": secretURL, "format": "surge"})
	resp, _ := http.Post(hs.URL+"/api/snell/sources", "application/json", bytes.NewReader(add))
	var src models.SnellSource
	json.NewDecoder(resp.Body).Decode(&src)
	resp.Body.Close()
	poolBody, _ := json.Marshal(map[string]any{"include_source_ids": []string{src.ID}, "managed_proxy_enabled": true})
	req, _ := http.NewRequest(http.MethodPut, hs.URL+"/api/snell/pool", bytes.NewReader(poolBody))
	req.Header.Set("Content-Type", "application/json")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if err := st.UpsertMiner(&models.AgentReport{MinerID: "n", WorkerID: "w1", Hostname: "h", Capabilities: []string{"snell-managed"}}); err != nil {
		t.Fatal(err)
	}

	cat, _ := http.Get(hs.URL + "/api/snell/catalog")
	raw, _ := io.ReadAll(cat.Body)
	cat.Body.Close()
	if strings.Contains(string(raw), "secret-a") || strings.Contains(string(raw), "secret-b") || strings.Contains(string(raw), "VerySecret") {
		t.Fatalf("catalog leaked psk: %s", raw)
	}
	if strings.Contains(string(raw), "token=VerySecret") || strings.Contains(string(raw), secretURL) {
		t.Fatalf("catalog leaked url")
	}

	req, _ = http.NewRequest(http.MethodGet, hs.URL+"/api/miners/w1/snell/pending", nil)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != 401 {
		t.Fatalf("missing bearer %d", resp.StatusCode)
	}
	resp.Body.Close()

	req, _ = http.NewRequest(http.MethodGet, hs.URL+"/api/miners/w1/snell/pending", nil)
	req.Header.Set("Authorization", "Bearer secret-key")
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != 200 {
		t.Fatalf("pending %d", resp.StatusCode)
	}
	raw, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(raw), "subscribe") || strings.Contains(string(raw), secretURL) || strings.Contains(string(raw), "VerySecret") {
		t.Fatalf("pending contained url: %s", raw)
	}
	var doc snellspec.ManagedDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.PolicyVersion == 0 || len(doc.Nodes) == 0 || doc.Nodes[0].PSK == "" {
		t.Fatalf("pending snapshot %+v", doc)
	}
}

func TestSnellEmptyAgentKeyRefusesPush(t *testing.T) {
	_, hs, _ := testAPI(t, "")
	req, _ := http.NewRequest(http.MethodGet, hs.URL+"/api/miners/x/snell/pending", nil)
	req.Header.Set("Authorization", "Bearer x")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("empty agent key status %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "managed-push refused") {
		t.Fatalf("body %s", b)
	}
}

func TestSnellSSRFRejects(t *testing.T) {
	f := snell.NewFetcher(snell.Limits{Timeout: 2 * time.Second})
	_, err := f.Get(t.Context(), "http://example.invalid/x", "", "")
	if snellspec.CodeOf(err) != "https_required" {
		t.Fatalf("http: %v", err)
	}
	_, err = f.Get(t.Context(), "https://127.0.0.1/", "", "")
	if snellspec.CodeOf(err) != "blocked_destination" {
		t.Fatalf("loopback: %v", err)
	}

	evil := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, surgeBody())
	}))
	defer evil.Close()
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL, http.StatusFound)
	}))
	defer origin.Close()
	lf := loopbackFetcher(origin)
	_, err = lf.Get(t.Context(), origin.URL, "", "")
	if snellspec.CodeOf(err) != "redirect_escape" && snellspec.CodeOf(err) != "fetch_failed" {
		t.Fatalf("redirect: %v", err)
	}
}

func TestSnellInheritPendingFollowsPoolToggle(t *testing.T) {
	_, hs, _ := testAPI(t, "secret-key")

	report, _ := json.Marshal(models.AgentReport{
		MinerID: "n", WorkerID: "w-follow", Hostname: "h",
		Capabilities: []string{"snell-managed"},
	})
	req, _ := http.NewRequest(http.MethodPost, hs.URL+"/api/report", bytes.NewReader(report))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("report %d %s", resp.StatusCode, b)
	}
	resp.Body.Close()

	pending := func() snellspec.ManagedDocument {
		t.Helper()
		r, _ := http.NewRequest(http.MethodGet, hs.URL+"/api/miners/w-follow/snell/pending", nil)
		r.Header.Set("Authorization", "Bearer secret-key")
		out, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer out.Body.Close()
		raw, _ := io.ReadAll(out.Body)
		if out.StatusCode != 200 {
			t.Fatalf("pending %d %s", out.StatusCode, raw)
		}
		var doc snellspec.ManagedDocument
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		return doc
	}

	if doc := pending(); doc.Enabled {
		t.Fatalf("invalid pool pending enabled: %+v", doc)
	}

	imp, _ := json.Marshal(map[string]string{
		"url":      "https://example.invalid/sub",
		"format":   "surge",
		"document": surgeBody(),
		"name":     "captured",
	})
	resp, err = http.Post(hs.URL+"/api/snell/sources/import-document", "application/json", bytes.NewReader(imp))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("import %d %s", resp.StatusCode, raw)
	}
	var imported struct {
		Source models.SnellSource `json:"source"`
	}
	if err := json.Unmarshal(raw, &imported); err != nil {
		t.Fatal(err)
	}
	poolBody, _ := json.Marshal(map[string]any{
		"include_source_ids":    []string{imported.Source.ID},
		"managed_proxy_enabled": true,
	})
	req, _ = http.NewRequest(http.MethodPut, hs.URL+"/api/snell/pool", bytes.NewReader(poolBody))
	req.Header.Set("Content-Type", "application/json")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()

	on := pending()
	if !on.Enabled {
		t.Fatalf("expected pending.enabled=true after pool on, got %+v", on)
	}
	if len(on.Nodes) == 0 {
		t.Fatal("expected pool nodes in pending")
	}

	poolBody, _ = json.Marshal(map[string]any{
		"include_source_ids":    []string{imported.Source.ID},
		"managed_proxy_enabled": false,
	})
	req, _ = http.NewRequest(http.MethodPut, hs.URL+"/api/snell/pool", bytes.NewReader(poolBody))
	req.Header.Set("Content-Type", "application/json")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()

	off := pending()
	if off.Enabled {
		t.Fatalf("expected pending.enabled=false after pool off, got %+v", off)
	}
}

func TestSnellMinerPutBumpsPendingAndApply(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TARISH_HOME", home)
	t.Setenv("TARISH_USER", "tester")
	t.Setenv("SUDO_USER", "")

	_, hs, _ := testAPI(t, "secret-key")
	imp, _ := json.Marshal(map[string]string{
		"url":      "https://example.invalid/sub",
		"format":   "surge",
		"document": surgeBody(),
		"name":     "captured",
	})
	resp, err := http.Post(hs.URL+"/api/snell/sources/import-document", "application/json", bytes.NewReader(imp))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("import %d %s", resp.StatusCode, raw)
	}
	var imported struct {
		Source models.SnellSource `json:"source"`
	}
	if err := json.Unmarshal(raw, &imported); err != nil {
		t.Fatal(err)
	}
	poolBody, _ := json.Marshal(map[string]any{
		"include_source_ids":    []string{imported.Source.ID},
		"managed_proxy_enabled": true,
	})
	req, _ := http.NewRequest(http.MethodPut, hs.URL+"/api/snell/pool", bytes.NewReader(poolBody))
	req.Header.Set("Content-Type", "application/json")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()

	report, _ := json.Marshal(models.AgentReport{
		MinerID: "n", WorkerID: "w-apply", Hostname: "h",
		Capabilities: []string{"snell-managed"},
	})
	req, _ = http.NewRequest(http.MethodPost, hs.URL+"/api/report", bytes.NewReader(report))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret-key")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("report %d", resp.StatusCode)
	}

	getPending := func() (snellspec.ManagedDocument, []byte) {
		t.Helper()
		r, _ := http.NewRequest(http.MethodGet, hs.URL+"/api/miners/w-apply/snell/pending", nil)
		r.Header.Set("Authorization", "Bearer secret-key")
		out, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer out.Body.Close()
		body, _ := io.ReadAll(out.Body)
		if out.StatusCode != 200 {
			t.Fatalf("pending %d %s", out.StatusCode, body)
		}
		var doc snellspec.ManagedDocument
		if err := json.Unmarshal(body, &doc); err != nil {
			t.Fatal(err)
		}
		return doc, body
	}

	applyDoc := func(doc snellspec.ManagedDocument) bool {
		t.Helper()
		ps, err := proxy.OpenStore()
		if err != nil {
			t.Fatal(err)
		}
		applied, err := proxy.ApplyManagedSnapshot(ps, doc.Clone(), proxy.ApplyHooks{})
		if err != nil {
			t.Fatalf("apply: %v", err)
		}
		return applied
	}

	first, _ := getPending()
	if !first.Enabled || first.PolicyVersion < 1 {
		t.Fatalf("first pending %+v", first)
	}
	if !applyDoc(first) {
		t.Fatal("first apply should write managed.json")
	}
	n := first.PolicyVersion

	put, _ := json.Marshal(map[string]any{"mode": "inherit", "enabled": false})
	req, _ = http.NewRequest(http.MethodPut, hs.URL+"/api/snell/miners/w-apply", bytes.NewReader(put))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("put inherit %d", resp.StatusCode)
	}

	off, _ := getPending()
	if off.Enabled {
		t.Fatalf("PUT enabled=false still pending enabled: %+v", off)
	}
	if off.PolicyVersion <= n {
		t.Fatalf("policy_version not bumped: %d -> %d", n, off.PolicyVersion)
	}
	if !applyDoc(off) {
		t.Fatal("enable switch snapshot must apply, not no-op")
	}

	nodesResp, err := http.Get(hs.URL + "/api/snell/nodes")
	if err != nil {
		t.Fatal(err)
	}
	var nodes []*models.SnellNodeView
	json.NewDecoder(nodesResp.Body).Decode(&nodes)
	nodesResp.Body.Close()
	var ids []string
	for _, node := range nodes {
		if node.Builtin {
			continue
		}
		ids = append(ids, node.ID)
	}
	if len(ids) < 2 {
		t.Fatalf("need two catalog nodes, got %d", len(ids))
	}

	put, _ = json.Marshal(map[string]any{"mode": "custom", "enabled": true, "custom_node_ids": []string{ids[0]}})
	req, _ = http.NewRequest(http.MethodPut, hs.URL+"/api/snell/miners/w-apply", bytes.NewReader(put))
	req.Header.Set("Content-Type", "application/json")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	custom1, _ := getPending()
	if !applyDoc(custom1) {
		t.Fatal("custom apply")
	}
	cver := custom1.PolicyVersion

	put, _ = json.Marshal(map[string]any{"mode": "custom", "enabled": true, "custom_node_ids": []string{ids[1]}})
	req, _ = http.NewRequest(http.MethodPut, hs.URL+"/api/snell/miners/w-apply", bytes.NewReader(put))
	req.Header.Set("Content-Type", "application/json")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	custom2, _ := getPending()
	if custom2.PolicyVersion <= cver {
		t.Fatalf("custom node pick did not bump version %d -> %d", cver, custom2.PolicyVersion)
	}
	if len(custom2.Nodes) != 1 || custom2.Nodes[0].ID != ids[1] {
		t.Fatalf("custom nodes %+v want %s", custom2.Nodes, ids[1])
	}
	if !applyDoc(custom2) {
		t.Fatal("custom node-id change must apply")
	}
	n = custom2.PolicyVersion

	type step struct {
		name         string
		do           func()
		want         func(doc snellspec.ManagedDocument)
		clearManaged bool
	}
	run := func(st step) {
		t.Helper()
		st.do()
		doc, _ := getPending()
		if doc.PolicyVersion <= n {
			t.Fatalf("%s policy_version not bumped: %d -> %d", st.name, n, doc.PolicyVersion)
		}
		st.want(doc)
		if !applyDoc(doc) {
			t.Fatalf("%s apply must not no-op", st.name)
		}
		ps, err := proxy.OpenStore()
		if err != nil {
			t.Fatal(err)
		}
		if st.clearManaged && ps.ManagedActive() {
			t.Fatalf("%s left managed.json active", st.name)
		}
		n = doc.PolicyVersion
	}

	run(step{
		name: "PUT mode=local",
		do: func() {
			put, _ := json.Marshal(map[string]any{"mode": "local"})
			req, _ := http.NewRequest(http.MethodPut, hs.URL+"/api/snell/miners/w-apply", bytes.NewReader(put))
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Fatalf("put local %d", resp.StatusCode)
			}
		},
		want: func(doc snellspec.ManagedDocument) {
			if doc.Mode != snellspec.ModeLocal {
				t.Fatalf("mode %s", doc.Mode)
			}
		},
		clearManaged: true,
	})

	run(step{
		name: "PUT inherit re-enter",
		do: func() {
			put, _ := json.Marshal(map[string]any{"mode": "inherit"})
			req, _ := http.NewRequest(http.MethodPut, hs.URL+"/api/snell/miners/w-apply", bytes.NewReader(put))
			req.Header.Set("Content-Type", "application/json")
			resp, _ := http.DefaultClient.Do(req)
			resp.Body.Close()
		},
		want: func(doc snellspec.ManagedDocument) {
			if doc.Mode != snellspec.ModeInherit {
				t.Fatalf("mode %s", doc.Mode)
			}
		},
	})

	run(step{
		name: "POST exit-managed",
		do: func() {
			resp, err := http.Post(hs.URL+"/api/snell/miners/w-apply/exit", "application/json", nil)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Fatalf("exit %d", resp.StatusCode)
			}
		},
		want: func(doc snellspec.ManagedDocument) {
			if doc.Mode != snellspec.ModeLocal {
				t.Fatalf("exit mode %s", doc.Mode)
			}
		},
		clearManaged: true,
	})

	run(step{
		name: "PUT inherit after exit",
		do: func() {
			put, _ := json.Marshal(map[string]any{"mode": "inherit"})
			req, _ := http.NewRequest(http.MethodPut, hs.URL+"/api/snell/miners/w-apply", bytes.NewReader(put))
			req.Header.Set("Content-Type", "application/json")
			resp, _ := http.DefaultClient.Do(req)
			resp.Body.Close()
		},
		want: func(doc snellspec.ManagedDocument) {
			if doc.Mode != snellspec.ModeInherit {
				t.Fatalf("mode %s", doc.Mode)
			}
		},
	})

	run(step{
		name: "disable catalog node",
		do: func() {
			off := false
			body, _ := json.Marshal(map[string]any{"enabled": off})
			req, _ := http.NewRequest(http.MethodPut, hs.URL+"/api/snell/nodes/"+ids[0], bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Fatalf("disable node %d", resp.StatusCode)
			}
		},
		want: func(doc snellspec.ManagedDocument) {
			for _, node := range doc.Nodes {
				if node.ID == ids[0] {
					t.Fatalf("disabled node still in snapshot")
				}
			}
		},
	})

	run(step{
		name: "UpdatePool managed off",
		do: func() {
			poolBody, _ := json.Marshal(map[string]any{
				"include_source_ids":    []string{imported.Source.ID},
				"managed_proxy_enabled": false,
			})
			req, _ := http.NewRequest(http.MethodPut, hs.URL+"/api/snell/pool", bytes.NewReader(poolBody))
			req.Header.Set("Content-Type", "application/json")
			resp, _ := http.DefaultClient.Do(req)
			resp.Body.Close()
		},
		want: func(doc snellspec.ManagedDocument) {
			if doc.Enabled {
				t.Fatal("pool off should disable inherit")
			}
		},
	})
}

func TestSnellImportDocumentAndManualNode(t *testing.T) {
	_, hs, st := testAPI(t, "k")
	body, _ := json.Marshal(map[string]string{
		"url":      "https://example.invalid/sub",
		"format":   "surge",
		"document": surgeBody(),
		"name":     "captured",
	})
	resp, err := http.Post(hs.URL+"/api/snell/sources/import-document", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("import %d %s", resp.StatusCode, b)
	}
	resp.Body.Close()
	nodes, _ := st.ListNodeViews()
	if len(nodes) < 2 {
		t.Fatalf("imported %d", len(nodes))
	}
	add, _ := json.Marshal(map[string]any{"name": "manual", "host": "198.51.100.9", "port": 443, "psk": "manpsk", "version": "v5"})
	resp, _ = http.Post(hs.URL+"/api/snell/nodes", "application/json", bytes.NewReader(add))
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("manual %d %s", resp.StatusCode, b)
	}
	resp.Body.Close()
}
