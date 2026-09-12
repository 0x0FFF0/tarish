package xmrig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"tarish/cpu"
)

func TestManagedRuntimeProxyAndNonProxy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TARISH_HOME", home)
	t.Setenv("TARISH_USER", "tester")
	t.Setenv("SUDO_USER", "")

	cfgDir := filepath.Join(home, ".local", "share", "tarish", "configs")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(cfgDir, "cpu.json")
	srcJSON := `{
	  "api": {"id": null, "worker-id": null},
	  "http": {"enabled": true, "host": "0.0.0.0", "port": 8181, "access-token": "Hello2025@"},
	  "donate-level": 5,
	  "pools": [{"url": "173.249.207.156:3333", "user": "wallet", "pass": "x", "socks5": null}]
	}`
	if err := os.WriteFile(src, []byte(srcJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	cpuInfo := &cpu.Info{Family: "apple_m1_pro"}
	priv := filepath.Join(home, ".local", "share", "tarish", "private")
	rtDir := filepath.Join(priv, "runtime")
	tok := filepath.Join(priv, "http_token")

	onPath, err := PrepareManagedRuntime(src, cpuInfo, ManagedOptions{
		ProxyEnabled: true,
		SOCKSAddr:    "127.0.0.1:19080",
		RuntimeDir:   rtDir,
		TokenPath:    tok,
	})
	if err != nil {
		t.Fatal(err)
	}
	on := readRaw(t, onPath)
	pools := on["pools"].([]interface{})
	if len(pools) != 1 {
		t.Fatalf("proxy mode must not keep plaintext fallback, got %d pools", len(pools))
	}
	p0 := pools[0].(map[string]interface{})
	if p0["url"] != TLSPoolURL {
		t.Fatalf("url=%v want %s", p0["url"], TLSPoolURL)
	}
	if p0["tls"] != true || p0["tls-fingerprint"] != TLSFingerprint {
		t.Fatalf("tls/pin %+v", p0)
	}
	if p0["socks5"] != "127.0.0.1:19080" {
		t.Fatalf("socks5=%v", p0["socks5"])
	}
	if on["donate-level"].(float64) != 0 {
		t.Fatalf("donate-level=%v", on["donate-level"])
	}
	httpSection := on["http"].(map[string]interface{})
	if httpSection["host"] != "127.0.0.1" {
		t.Fatalf("http host %v", httpSection["host"])
	}
	if httpSection["access-token"] == "Hello2025@" || httpSection["access-token"] == "" {
		t.Fatal("token was not replaced")
	}

	offPath, err := PrepareManagedRuntime(src, cpuInfo, ManagedOptions{RuntimeDir: rtDir, TokenPath: tok})
	if err != nil {
		t.Fatal(err)
	}
	off := readRaw(t, offPath)
	offPools := off["pools"].([]interface{})
	if len(offPools) < 1 {
		t.Fatal("non-proxy pools missing")
	}

	if _, err := PrepareManagedRuntime("/no/such/config.json", cpuInfo, ManagedOptions{}); err == nil {
		t.Fatal("missing config must fail, not fall back")
	}

	if TLSPoolURL != "173.249.207.156:2083" || NonTLSPoolURL != "173.249.207.156:3333" {
		t.Fatalf("pool endpoints changed: %s %s", TLSPoolURL, NonTLSPoolURL)
	}
	if TLSFingerprint != "A1562B61C08AE4DE4218D0637A9E44984C11750A2F4C81D308A61D8553F19FD7" {
		t.Fatalf("pin changed: %s", TLSFingerprint)
	}
}

func TestSanitizeOverrideRejectsProtected(t *testing.T) {
	current := map[string]interface{}{
		"pools":        []interface{}{map[string]interface{}{"url": TLSPoolURL, "socks5": "127.0.0.1:1"}},
		"http":         map[string]interface{}{"host": "127.0.0.1", "access-token": "tok"},
		"donate-level": 0,
		"cpu":          map[string]interface{}{"priority": 5},
	}
	override := map[string]interface{}{
		"pools":        []interface{}{map[string]interface{}{"url": "evil:3333"}},
		"donate-level": 1,
		"cpu":          map[string]interface{}{"priority": 1},
	}
	out, rejected := SanitizeOverride(override, current, true)
	if len(rejected) == 0 {
		t.Fatal("expected rejected fields")
	}
	if v, ok := out["donate-level"].(int); !ok || v != 0 {
		if f, ok := out["donate-level"].(float64); !ok || f != 0 {
			t.Fatalf("donate-level=%v", out["donate-level"])
		}
	}
	if !OverrideEnablesDonation(override) {
		t.Fatal("donation override not detected")
	}
}

func readRaw(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	return raw
}
