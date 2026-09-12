package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"tarish/config"
	"tarish/xmrig"
)

type recordingMiner struct {
	mu         sync.Mutex
	cmd        *exec.Cmd
	starts     int
	stopped    int
	lastConfig string
	lastPath   string
	pids       []int
}

func (m *recordingMiner) Start(binary, cfg string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd != nil && m.cmd.Process != nil {
		_ = syscall.Kill(-m.cmd.Process.Pid, syscall.SIGKILL)
		_, _ = m.cmd.Process.Wait()
		m.cmd = nil
	}
	data, err := os.ReadFile(cfg)
	if err != nil {
		return 0, err
	}
	m.lastConfig = string(data)
	m.lastPath = cfg
	m.starts++
	cmd := exec.Command("sleep", "120")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	m.cmd = cmd
	m.pids = append(m.pids, cmd.Process.Pid)
	return cmd.Process.Pid, nil
}

func (m *recordingMiner) Stop(pid int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped++
	if pid > 0 {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		pgid, err := syscall.Getpgid(pid)
		if err == nil {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		}
	}
	if m.cmd != nil && m.cmd.Process != nil && m.cmd.Process.Pid == pid {
		_, _ = m.cmd.Process.Wait()
		m.cmd = nil
	}
	return nil
}

func (m *recordingMiner) LastConfig() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastConfig
}

func (m *recordingMiner) Starts() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.starts
}

func writeMinerConfig(t *testing.T) {
	t.Helper()
	cfgDir := filepath.Join(os.Getenv("TARISH_HOME"), ".local", "share", "tarish", "configs")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logDir := filepath.Join(os.Getenv("TARISH_HOME"), ".local", "share", "tarish", "log")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `{
	  "api": {"id": null, "worker-id": null},
	  "http": {"enabled": true, "host": "127.0.0.1", "port": 8181, "access-token": "x"},
	  "donate-level": 0,
	  "pools": [{"url": "173.249.207.156:3333", "user": "wallet", "pass": "x", "socks5": null}]
	}`
	if err := os.WriteFile(filepath.Join(cfgDir, "default.json"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeSnapshot(t *testing.T, store *Store, validated time.Time) {
	t.Helper()
	doc := "[Proxy]\nN = snell, 198.51.100.1, 440, psk=testpsk, version=4\n"
	snap, err := ParseDocument([]byte(doc), FormatSurge)
	if err != nil {
		t.Fatal(err)
	}
	snap.FetchedAt = validated
	snap.ValidatedAt = validated
	ps := &persistedStore{
		Format:         "surge",
		StaticDocument: true,
		FetchedAt:      validated,
		ValidatedAt:    validated,
		Snapshot:       snapshotPersist(snap),
	}
	if err := store.Save(ps); err != nil {
		t.Fatal(err)
	}
	_ = store.SaveCache(ps)
}

func startTestDaemon(t *testing.T, miner *recordingMiner) (*Daemon, chan error) {
	t.Helper()
	d := NewDaemon()
	d.StartMiner = miner.Start
	d.StopMiner = miner.Stop
	d.FindBinary = func() (string, error) { return "sleep", nil }
	done := make(chan error, 1)
	go func() { done <- d.Run() }()
	t.Cleanup(func() {
		d.Shutdown()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
		if miner.cmd != nil && miner.cmd.Process != nil {
			_ = miner.Stop(miner.cmd.Process.Pid)
		}
	})
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("Run exited before ready: %v (%s)", err, ErrorCode(err))
		default:
		}
		resp, err := DialControl("ready", 200*time.Millisecond)
		if err == nil && resp.Ready {
			return d, done
		}
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatal("daemon not ready")
	return d, done
}

func waitDaemonReady(t *testing.T, timeout time.Duration) *ctlResp {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		resp, err := DialControl("ready", 300*time.Millisecond)
		if err == nil && resp.Ready {
			return resp
		}
		last = err
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatalf("daemon not ready: %v", last)
	return nil
}

func TestDaemonEnableDisableStartsAndStopsSOCKS(t *testing.T) {
	testHome(t)
	writeMinerConfig(t)
	store, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	writeSnapshot(t, store, time.Now())
	if err := config.SetProxyEnabled(false); err != nil {
		t.Fatal(err)
	}

	miner := &recordingMiner{}
	d, _ := startTestDaemon(t, miner)
	if d.socks != nil {
		t.Fatal("SOCKS listener started while proxy disabled")
	}

	var out, errw bytes.Buffer
	if err := HandleIO([]string{"enable"}, strings.NewReader(""), &out, &errw); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if d.socks == nil {
		t.Fatal("SOCKS still nil after enable")
	}
	addr := d.socks.Addr()
	if addr == "" || !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("socks addr %q", addr)
	}
	cfg := miner.LastConfig()
	if !strings.Contains(cfg, `"socks5"`) || !strings.Contains(cfg, "127.0.0.1:") {
		t.Fatalf("runtime config missing bound SOCKS: %s", cfg)
	}
	if strings.Count(cfg, `"url"`) > 1 && strings.Contains(cfg, "3333") && strings.Contains(cfg, `"socks5": null`) {
		t.Fatal("plaintext fallback pool present in proxy mode")
	}

	if err := HandleIO([]string{"disable"}, strings.NewReader(""), &out, &errw); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if d.socks != nil {
		t.Fatal("SOCKS still up after disable")
	}
	if d.router != nil {
		t.Fatal("router still live after disable")
	}
	off := miner.LastConfig()
	if strings.Contains(off, "127.0.0.1:") && strings.Contains(off, `"socks5": "127.0.0.1`) {
		t.Fatalf("disable left proxy socks in miner config: %s", off)
	}
}

func TestDaemonExpiredCacheNotLoadedForNewDials(t *testing.T) {
	testHome(t)
	writeMinerConfig(t)
	store, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	writeSnapshot(t, store, time.Now().Add(-8*24*time.Hour))
	if err := config.SetProxyEnabled(true); err != nil {
		t.Fatal(err)
	}
	miner := &recordingMiner{}
	d, _ := startTestDaemon(t, miner)
	if d.socks == nil {
		t.Fatal("SOCKS should still bind when cache is expired")
	}
	if d.router == nil {
		t.Fatal("router missing")
	}
	if d.router.CacheExpired() {
		t.Fatal("seed node should stay eligible when subscription cache is expired")
	}
	for _, st := range d.router.nodes {
		if st.spec.Host == "198.51.100.1" {
			t.Fatal("expired subscription node loaded for new connections")
		}
	}
	if _, ok := d.router.nodes[BootstrapNode().ID]; !ok {
		t.Fatal("expected built-in seed node after expired boot")
	}
}

func TestDaemonExpiredThenRefreshUsesNodes(t *testing.T) {
	testHome(t)
	writeMinerConfig(t)
	store, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	writeSnapshot(t, store, time.Now().Add(-8*24*time.Hour))
	if err := config.SetProxyEnabled(true); err != nil {
		t.Fatal(err)
	}
	miner := &recordingMiner{}
	d, _ := startTestDaemon(t, miner)
	if _, ok := d.router.nodes["198.51.100.1"]; ok {
		t.Fatal("precondition: expired subscription host must not be loaded")
	}
	for _, st := range d.router.nodes {
		if st.spec.Host == "198.51.100.1" {
			t.Fatal("precondition: expired subscription node loaded")
		}
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(io.Discard, c)
			}(c)
		}
	}()
	host, port, _ := SplitHostPort(ln.Addr().String())

	var nodeDials atomic.Int32
	d.router.newDialer = func(spec NodeSpec) (Dialer, error) {
		return &countDialer{n: &nodeDials}, nil
	}

	resp, err := DialControl("refresh", 5*time.Second)
	if err != nil || !resp.OK {
		t.Fatalf("refresh: %v %+v", err, resp)
	}
	if d.router.CacheExpired() {
		t.Fatal("successful refresh left cache expired")
	}
	if len(d.router.order) == 0 {
		t.Fatal("successful refresh did not load nodes")
	}

	c, err := d.router.Dial(context.Background(), host, port)
	if c != nil {
		_ = c.Close()
	}
	_ = err
	if nodeDials.Load() == 0 {
		t.Fatal("after refresh, new dials still skipped snell nodes")
	}
}

func TestDaemonStalePIDOwnedOrphanForeignAndOccupiedPort(t *testing.T) {
	testHome(t)
	writeMinerConfig(t)
	store, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	writeSnapshot(t, store, time.Now())
	if err := config.SetProxyEnabled(false); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(store.SupervisorPIDPath(), []byte("999999\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	orphan := exec.Command("sleep", "60")
	orphan.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := orphan.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = syscall.Kill(-orphan.Process.Pid, syscall.SIGKILL)
		_, _ = orphan.Process.Wait()
	}()
	if err := os.MkdirAll(filepath.Dir(store.MinerPIDPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.MinerPIDPath(), []byte(strconv.Itoa(orphan.Process.Pid)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	foreign := exec.Command("sleep", "60")
	foreign.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := foreign.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = syscall.Kill(-foreign.Process.Pid, syscall.SIGKILL)
		_, _ = foreign.Process.Wait()
	}()
	if err := os.MkdirAll(filepath.Dir(xmrig.GetPIDFile()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(xmrig.GetPIDFile(), []byte(strconv.Itoa(foreign.Process.Pid)), 0o644); err != nil {
		t.Fatal(err)
	}

	httpLn, err := net.Listen("tcp", "127.0.0.1:8181")
	if err != nil {
		t.Skip("could not occupy 8181")
	}
	defer httpLn.Close()

	miner := &recordingMiner{}
	d, done := startTestDaemon(t, miner)

	orphanDone := make(chan struct{})
	go func() {
		_, _ = orphan.Process.Wait()
		close(orphanDone)
	}()
	select {
	case <-orphanDone:
	case <-time.After(2 * time.Second):
		t.Fatal("owned orphan miner still alive after boot")
	}
	if err := foreign.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("foreign process was killed")
	}

	raw := miner.LastConfig()
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatal(err)
	}
	httpSection, _ := parsed["http"].(map[string]interface{})
	port, _ := httpSection["port"].(float64)
	if int(port) == 8181 {
		t.Fatal("occupied HTTP port was not relocated")
	}

	resp, err := DialControl("stop", 3*time.Second)
	if err != nil || !resp.OK {
		t.Fatalf("stop: %v %+v", err, resp)
	}
	select {
	case runErr := <-done:
		if runErr != nil {
			t.Fatalf("Run after stop: %v", runErr)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("daemon did not exit after stop")
	}
	if d.socks != nil {
		t.Fatal("SOCKS still bound after clean stop")
	}
}

func TestDaemonLockRejectsDuplicate(t *testing.T) {
	testHome(t)
	writeMinerConfig(t)
	store, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.SetProxyEnabled(false); err != nil {
		t.Fatal(err)
	}
	_ = store
	miner := &recordingMiner{}
	startTestDaemon(t, miner)

	d2 := NewDaemon()
	d2.StartMiner = miner.Start
	d2.StopMiner = miner.Stop
	d2.FindBinary = func() (string, error) { return "sleep", nil }
	err = d2.Run()
	if ErrorCode(err) != "already_running" {
		t.Fatalf("duplicate Run: %v", err)
	}
}
