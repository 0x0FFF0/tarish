package proxy

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tarish/cpu"
	"tarish/xmrig"
)

const (
	sentinelURL  = "https://sentinel.example.invalid/sub?token=SENTINEL_URL_TOKEN_9f3a"
	sentinelPSK  = "SENTINEL_PSK_c0ffee_deadbeef"
	sentinelHost = "203.0.113.77"
	sentinelName = "sentinel-node-name"
)

func TestSecretsNeverLeak(t *testing.T) {
	testHome(t)
	store, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	doc := "[Proxy]\n" + sentinelName + " = snell, " + sentinelHost + ", 440, psk=\"" + sentinelPSK + "\", version=4\n"
	snap, err := ParseDocument([]byte(doc), FormatSurge)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	snap.FetchedAt = now
	snap.ValidatedAt = now
	ps := &persistedStore{
		SubscriptionURL: sentinelURL,
		Format:          "surge",
		StaticDocument:  true,
		FetchedAt:       now,
		ValidatedAt:     now,
		Snapshot:        snapshotPersist(snap),
		LastErrorCode:   "html_document",
	}
	if err := store.Save(ps); err != nil {
		t.Fatal(err)
	}
	RememberSecrets(snap, sentinelURL)

	var logs bytes.Buffer
	lg := RedactingLogger(&logs)
	lg.Info("refresh failed", "err", "connection to "+sentinelHost+" token="+sentinelURL+" psk="+sentinelPSK)

	st, err := currentStatus()
	if err != nil {
		t.Fatal(err)
	}
	var statusBuf bytes.Buffer
	printStatus(&statusBuf, st)

	cfgDir := filepath.Join(os.Getenv("TARISH_HOME"), ".local", "share", "tarish", "configs")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(cfgDir, "test.json")
	if err := os.WriteFile(src, []byte(`{"api":{},"http":{"enabled":true,"port":8181},"pools":[{"url":"173.249.207.156:3333","user":"wallet","pass":"x"}],"donate-level":0}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runtimePath, err := xmrig.PrepareManagedRuntime(src, &cpu.Info{Family: "apple_m1"}, xmrig.ManagedOptions{
		ProxyEnabled: true,
		SOCKSAddr:    "127.0.0.1:1080",
		RuntimeDir:   store.RuntimeDir(),
		TokenPath:    store.TokenPath(),
	})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := os.ReadFile(runtimePath)
	if err != nil {
		t.Fatal(err)
	}

	report := xmrig.RedactLiveConfig(map[string]interface{}{
		"http":  map[string]interface{}{"access-token": "secret-token", "host": "127.0.0.1"},
		"pools": []interface{}{map[string]interface{}{"url": "173.249.207.156:2083", "socks5": "127.0.0.1:1080"}},
	})
	repJSON, _ := json.Marshal(report)

	blobs := []string{logs.String(), statusBuf.String(), string(rt), string(repJSON), ErrorCode(errCode("html_document"))}
	for _, blob := range blobs {
		for _, s := range []string{sentinelURL, sentinelPSK, sentinelHost, sentinelName, "SENTINEL_URL_TOKEN_9f3a", BootstrapNode().PSK, BootstrapNode().Host} {
			if strings.Contains(blob, s) {
				t.Fatalf("sentinel %q leaked in %q", s, blob)
			}
		}
	}
}
