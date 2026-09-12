package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("TARISH_HOME", home)
	t.Setenv("TARISH_USER", "tester")
	t.Setenv("SUDO_USER", "")
}

func TestFetch200And304AndFailures(t *testing.T) {
	testHome(t)
	var hits int
	body := "[Proxy]\nN = snell, 198.51.100.9, 440, psk=fetchpsk, version=4\n"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("If-None-Match") == `"v1"` && hits > 1 {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if strings.Contains(r.URL.Path, "html") {
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, "<html>nope</html>")
			return
		}
		if strings.Contains(r.URL.Path, "empty") {
			io.WriteString(w, "[Proxy]\nX = ss, 1.1.1.1, 443, password=x\n")
			return
		}
		w.Header().Set("ETag", `"v1"`)
		io.WriteString(w, body)
	}))
	defer srv.Close()

	store, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	f := NewFetcher(store, FormatSurge)
	f.Bootstrap = func() (Dialer, error) { return nil, errCode("fetch_failed") }
	f.Client = IndependentHTTPClient(5 * time.Second)
	f.Client.Transport = srv.Client().Transport

	ps := &persistedStore{SubscriptionURL: srv.URL, Format: "surge"}
	if err := store.Save(ps); err != nil {
		t.Fatal(err)
	}
	snap, err := f.Refresh(t.Context())
	if err != nil || snap == nil || countNonBootstrap(snap) != 1 {
		t.Fatalf("200: snap=%v err=%v", snap, err)
	}

	snap2, err := f.Refresh(t.Context())
	if err != nil || snap2 == nil || countNonBootstrap(snap2) != 1 {
		t.Fatalf("304: snap=%v err=%v", snap2, err)
	}

	ps, err = store.Load()
	if err != nil || ps == nil {
		t.Fatalf("reload store: %v", err)
	}
	ps.SubscriptionURL = srv.URL + "/html"
	ps.ETag = ""
	_ = store.Save(ps)
	kept, err := f.Refresh(t.Context())
	if kept == nil || countNonBootstrap(kept) != 1 {
		t.Fatalf("html should keep last-known-good, got %v err=%v", kept, err)
	}
	if ErrorCode(err) != "html_document" {
		t.Fatalf("html code %s", ErrorCode(err))
	}

	ps, err = store.Load()
	if err != nil || ps == nil {
		t.Fatalf("reload store: %v", err)
	}
	ps.SubscriptionURL = srv.URL + "/empty"
	_ = store.Save(ps)
	kept, err = f.Refresh(t.Context())
	if kept == nil || countNonBootstrap(kept) != 1 {
		t.Fatalf("empty compatible set clobbered cache: %v %v", kept, err)
	}
}

func TestFetchRedirectEscapeAndInitialOutage(t *testing.T) {
	testHome(t)
	evil := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "[Proxy]\nN = snell, 198.51.100.9, 440, psk=x, version=4\n")
	}))
	defer evil.Close()
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL, http.StatusFound)
	}))
	defer origin.Close()

	store, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	f := NewFetcher(store, FormatSurge)
	f.Bootstrap = func() (Dialer, error) { return nil, errCode("fetch_failed") }
	f.Client = IndependentHTTPClient(5 * time.Second)
	f.Client.Transport = origin.Client().Transport
	ps := &persistedStore{SubscriptionURL: origin.URL, Format: "surge"}
	_ = store.Save(ps)
	_, err = f.Refresh(t.Context())
	if ErrorCode(err) != "redirect_escape" && ErrorCode(err) != "fetch_failed" {
		t.Fatalf("redirect escape got %s", ErrorCode(err))
	}

	ps.Snapshot = snapshotPersist(&Snapshot{Nodes: []NodeSpec{{ID: "abc", Host: "198.51.100.9", Port: 440, PSK: "x", Version: "v4"}}})
	ps.ValidatedAt = time.Now()
	_ = store.Save(ps)
	_ = store.SaveCache(ps)
	ps.SubscriptionURL = "https://127.0.0.1:1/outage"
	_ = store.Save(ps)
	kept, err := f.Refresh(t.Context())
	if kept == nil || countNonBootstrap(kept) != 1 {
		t.Fatalf("outage should keep last-known-good: %v %v", kept, err)
	}
}

func TestFetchFallsBackToBootstrapSnell(t *testing.T) {
	testHome(t)
	body := "[Proxy]\nN = snell, 198.51.100.9, 440, psk=fetchpsk, version=4\n"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	psk := "boot-test-psk"
	snellAddr, stop := testSnell(t, psk)
	defer stop()
	sh, sp, _ := SplitHostPort(snellAddr)

	store, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	f := NewFetcher(store, FormatSurge)
	f.Client = &http.Client{Timeout: 2 * time.Second, Transport: failRT{}}
	if tr, ok := srv.Client().Transport.(*http.Transport); ok {
		f.TLSConfig = tr.TLSClientConfig
	}
	f.Bootstrap = func() (Dialer, error) {
		return NewAdapter(NodeSpec{ID: "boot", Host: sh, Port: sp, PSK: psk, Version: "v4"})
	}
	ps := &persistedStore{SubscriptionURL: srv.URL, Format: "surge"}
	if err := store.Save(ps); err != nil {
		t.Fatal(err)
	}
	snap, err := f.Refresh(t.Context())
	if err != nil {
		t.Fatalf("bootstrap fetch: %v", err)
	}
	if countNonBootstrap(snap) != 1 {
		t.Fatalf("non-bootstrap nodes=%d snap=%+v", countNonBootstrap(snap), snap.Nodes)
	}
	if !containsBootstrap(snap) {
		t.Fatal("seed node missing from snapshot")
	}
}

func containsBootstrap(snap *Snapshot) bool {
	if snap == nil {
		return false
	}
	for _, n := range snap.Nodes {
		if isBootstrap(n) {
			return true
		}
	}
	return false
}

type failRT struct{}

func (failRT) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errCode("fetch_failed")
}

func TestUsableSnapshotExpiredNotEligible(t *testing.T) {
	ps := &persistedStore{
		ValidatedAt: time.Now().Add(-8 * 24 * time.Hour),
		Snapshot:    snapshotPersist(&Snapshot{Nodes: []NodeSpec{{ID: "n", Host: "203.0.113.1", Port: 1, PSK: "x", Version: "v4"}}}),
	}
	snap, stale, expired, err := UsableSnapshot(ps, time.Now())
	if snap == nil {
		t.Fatal("snapshot bytes still returned for inspection")
	}
	if !expired || !stale {
		t.Fatalf("expired=%v stale=%v", expired, stale)
	}
	if ErrorCode(err) != "cache_expired" {
		t.Fatalf("err %s", ErrorCode(err))
	}
}

func TestAtomicWriteFailure(t *testing.T) {
	testHome(t)
	store, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	dir := store.Dir()
	link := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(link, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = atomicWrite(filepath.Join(target, "x.json"), []byte("{}"), 0o600)
	if err == nil {
		t.Fatal("expected atomic write failure")
	}
}

func TestStoreRejectsSymlink(t *testing.T) {
	testHome(t)
	store, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(t.TempDir(), "secret.json")
	if err := os.WriteFile(real, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.Dir(), subscriptionFile)
	_ = os.Remove(path)
	if err := os.Symlink(real, path); err != nil {
		t.Fatal(err)
	}
	_, err = store.Load()
	if ErrorCode(err) != "symlink_rejected" && ErrorCode(err) != "store_io" {
		t.Fatalf("symlink: %v", err)
	}
}
