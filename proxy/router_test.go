package proxy

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"log/slog"
	"math/big"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/missuo/opensnell/components/snell"
)

func testTLSServer(t *testing.T) (addr, pin string, stop func()) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "tarish-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				close(done)
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(io.Discard, c)
			}(c)
		}
	}()
	return ln.Addr().String(), SHA256Hex(der), func() { _ = ln.Close(); <-done }
}

func testSnell(t *testing.T, psk string) (addr string, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := snell.NewServer(snell.ServerConfig{PSK: psk, ObfsMode: "off", QUIC: false, UDP: false}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go srv.ServeListener(ctx, ln)
	return ln.Addr().String(), func() { cancel(); _ = ln.Close() }
}

func TestProbeRequiresSnellPlusTLSPin(t *testing.T) {
	tlsAddr, pin, stopTLS := testTLSServer(t)
	defer stopTLS()
	psk := "probe-psk"
	snellAddr, stopSnell := testSnell(t, psk)
	defer stopSnell()

	plain, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	go func() {
		c, err := plain.Accept()
		if err == nil {
			defer c.Close()
			time.Sleep(200 * time.Millisecond)
		}
	}()

	th, tp, _ := SplitHostPort(tlsAddr)
	target := Target{Host: th, Port: tp}
	r := NewRouter(target, pin, DefaultPolicy(), realClock{})
	r.Enable(true)
	sh, sp, _ := SplitHostPort(snellAddr)
	r.ReplaceSnapshot(&Snapshot{Nodes: []NodeSpec{{
		ID: "n1", Host: sh, Port: sp, PSK: psk, Version: "v4",
	}}})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	got := r.ProbeOnce(ctx)
	if got["n1"] != "ok" {
		t.Fatalf("snell+tls probe = %v", got)
	}

	ph, pp, _ := SplitHostPort(plain.Addr().String())
	r2 := NewRouter(Target{Host: ph, Port: pp}, pin, DefaultPolicy(), realClock{})
	r2.Enable(true)
	r2.ReplaceSnapshot(&Snapshot{Nodes: []NodeSpec{{
		ID: "n2", Host: sh, Port: sp, PSK: psk, Version: "v4",
	}}})
	got = r2.ProbeOnce(ctx)
	if got["n2"] == "ok" {
		t.Fatal("plain TCP port-open counted as probe success")
	}
}

func TestBudgetExhaustionFallsBackDirect(t *testing.T) {
	tlsAddr, pin, stopTLS := testTLSServer(t)
	defer stopTLS()
	th, tp, _ := SplitHostPort(tlsAddr)
	clock := NewFakeClock(time.Now())
	policy := DefaultPolicy()
	policy.SelectBudget = 80 * time.Millisecond
	policy.ProbeTimeout = 40 * time.Millisecond
	r := NewRouter(Target{Host: th, Port: tp}, pin, policy, clock)
	r.Enable(true)
	r.newDialer = func(spec NodeSpec) (Dialer, error) {
		return &hangDialer{}, nil
	}
	r.ReplaceSnapshot(&Snapshot{Nodes: []NodeSpec{{ID: "slow", Host: "127.0.0.1", Port: 9, PSK: "x", Version: "v4"}}})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, err := r.Dial(ctx, th, tp)
	if err != nil {
		t.Fatalf("direct fallback: %v", err)
	}
	_ = c.Close()
	if r.State() != RouteDirectFallback {
		t.Fatalf("state %s", r.State())
	}
}

func TestRecoveryAndRetirement(t *testing.T) {
	tlsAddr, pin, stopTLS := testTLSServer(t)
	defer stopTLS()
	th, tp, _ := SplitHostPort(tlsAddr)
	psk := "rec-psk"
	snellAddr, stopSnell := testSnell(t, psk)
	defer stopSnell()
	sh, sp, _ := SplitHostPort(snellAddr)

	clock := NewFakeClock(time.Now())
	policy := DefaultPolicy()
	policy.RecoveryDirectMin = time.Minute
	policy.RecoverySuccesses = 3
	r := NewRouter(Target{Host: th, Port: tp}, pin, policy, clock)
	r.Enable(true)
	var reconnects atomic.Int32
	r.SetReconnect(func() { reconnects.Add(1) })
	spec := NodeSpec{ID: "keep", Host: sh, Port: sp, PSK: psk, Version: "v4", Name: "old"}
	r.ReplaceSnapshot(&Snapshot{Nodes: []NodeSpec{spec}})
	r.setDirect()

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		got := r.ProbeOnce(ctx)
		if got["keep"] != "ok" {
			t.Fatalf("probe %d %v", i, got)
		}
	}
	if r.State() != RouteDirectFallback {
		t.Fatalf("should stay direct until min duration, got %s", r.State())
	}
	clock.Advance(2 * time.Minute)
	_ = r.ProbeOnce(ctx)
	time.Sleep(20 * time.Millisecond)
	if r.State() != RouteProxied {
		t.Fatalf("expected recovery, got %s", r.State())
	}
	if reconnects.Load() != 1 {
		t.Fatalf("reconnects=%d want 1", reconnects.Load())
	}

	r.ReplaceSnapshot(&Snapshot{Nodes: []NodeSpec{{ID: "keep", Host: sh, Port: sp, PSK: psk, Version: "v4", Name: "renamed"}}})
	if r.State() != RouteProxied {
		t.Fatal("rename retired session")
	}
	r.ReplaceSnapshot(&Snapshot{Nodes: []NodeSpec{{ID: "keep", Host: sh, Port: sp, PSK: "other", Version: "v4", Name: "renamed"}}})
}

type hangDialer struct {
	mu    sync.Mutex
	conns []net.Conn
}

func (h *hangDialer) ID() string { return "hang" }

func (h *hangDialer) DialTCP(ctx context.Context, host string, port uint16) (net.Conn, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (h *hangDialer) Close() error { return nil }

func TestTargetWideTLSFailureNotSingleNodeBlame(t *testing.T) {
	tlsAddr, _, stopTLS := testTLSServer(t)
	defer stopTLS()
	psk := "pin-psk"
	snellAddr, stop := testSnell(t, psk)
	defer stop()
	sh, sp, _ := SplitHostPort(snellAddr)
	ph, pp, _ := SplitHostPort(tlsAddr)
	r := NewRouter(Target{Host: ph, Port: pp}, "DEADBEEFDEADBEEFDEADBEEFDEADBEEFDEADBEEFDEADBEEFDEADBEEFDEADBEEF", DefaultPolicy(), realClock{})
	r.Enable(true)
	r.ReplaceSnapshot(&Snapshot{Nodes: []NodeSpec{
		{ID: "a", Host: sh, Port: sp, PSK: psk, Version: "v4"},
		{ID: "b", Host: sh, Port: sp, PSK: psk, Version: "v4"},
	}})
	got := r.ProbeOnce(context.Background())
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.targetTLSFail {
		t.Fatalf("expected target-wide tls fail, probes=%v", got)
	}
	for _, st := range r.nodes {
		if st.cooldownUntil.After(time.Now()) {
			t.Fatal("node cooldown after target-wide tls failure")
		}
	}
}

func TestExpiredCacheSkipsNodesForNewDials(t *testing.T) {
	var nodeDials atomic.Int32
	var directDials atomic.Int32
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
	r := NewRouter(Target{Host: host, Port: port}, "", DefaultPolicy(), realClock{})
	r.Enable(true)
	r.directDial = func(ctx context.Context, addr string) (net.Conn, error) {
		directDials.Add(1)
		return defaultDirectDial(ctx, addr)
	}
	r.newDialer = func(spec NodeSpec) (Dialer, error) {
		return &countDialer{n: &nodeDials}, nil
	}
	r.ReplaceSnapshot(&Snapshot{Nodes: []NodeSpec{{
		ID: "n1", Host: "203.0.113.9", Port: 440, PSK: "x", Version: "v4",
	}}})
	r.SetCacheExpired(true)
	c, err := r.Dial(context.Background(), host, port)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if nodeDials.Load() != 0 {
		t.Fatalf("expired cache used snell for a new dial, nodeDials=%d", nodeDials.Load())
	}
	if directDials.Load() == 0 {
		t.Fatal("expected pinned-TLS direct fallback")
	}
	if r.State() != RouteDirectFallback {
		t.Fatalf("state %s", r.State())
	}
}

type countDialer struct{ n *atomic.Int32 }

func (c *countDialer) ID() string { return "count" }

func (c *countDialer) DialTCP(ctx context.Context, host string, port uint16) (net.Conn, error) {
	c.n.Add(1)
	return nil, errCode("retired")
}

func (c *countDialer) Close() error { return nil }
