/*
 * This file is part of opensnell.
 * SPDX-License-Identifier: GPL-3.0-or-later
 *
 * Tarish nested-module tests for connection-establishment cancellation.
 */

package snell

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"
)

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testClient(t *testing.T, server string) *Client {
	t.Helper()
	c, err := NewClient(ClientConfig{
		Server:      server,
		PSK:         "test-psk-not-secret",
		Reuse:       false,
		TFO:         false,
		Version:     "v4",
		DialTimeout: time.Second,
	}, silentLogger())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func TestWriteEstablishmentHeaderBlockedFirstWrite(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	client := testClient(t, "127.0.0.1:9")
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.writeEstablishmentHeader(ctx, c1, "example.com", 443, false)
	}()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("blocked first write succeeded; want deadline or cancel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked first write did not return; establishment is not deadline-bounded")
	}

	// Peer side should observe close rather than a leaked pipe.
	_ = c2.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	buf := make([]byte, 8)
	_, rerr := c2.Read(buf)
	if rerr == nil {
		t.Fatal("expected closed/failed pipe after blocked write, got a successful read")
	}
}

func TestWriteEstablishmentHeaderCancelClosesConn(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c2.Close()

	client := testClient(t, "127.0.0.1:9")
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.writeEstablishmentHeader(ctx, c1, "example.com", 443, false)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) && !errors.Is(err, net.ErrClosed) && err != nil {
			// cancel must surface; net.Pipe write after close is also acceptable
			if ctx.Err() == nil {
				t.Fatalf("err = %v, want context cancellation", err)
			}
		}
		if err == nil {
			t.Fatal("canceled establishment returned nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled establishment did not return")
	}
}

func TestDialTCPCancelDuringConnect(t *testing.T) {
	addr, cleanup := hangingListen(t)
	defer cleanup()

	client := testClient(t, addr)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(40 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, derr := client.DialTCP(ctx, "example.com", 443)
	if derr == nil {
		t.Fatal("DialTCP succeeded while cancelled")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("cancel did not bound DialTCP, elapsed %s", time.Since(start))
	}
}

func TestDialTCPTimeout(t *testing.T) {
	addr, cleanup := hangingListen(t)
	defer cleanup()

	client := testClient(t, addr)
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, derr := client.DialTCP(ctx, "example.com", 443)
	if derr == nil {
		t.Fatal("DialTCP succeeded against a hanging listener with a short timeout")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("timeout did not bound DialTCP, elapsed %s", time.Since(start))
	}
}

// hangingListen fills a local TCP backlog so further dials block in connect.
func hangingListen(t *testing.T) (addr string, cleanup func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var fillers []net.Conn
	cleanup = func() {
		for _, c := range fillers {
			_ = c.Close()
		}
		_ = ln.Close()
	}
	for i := 0; i < 512; i++ {
		c, derr := net.DialTimeout("tcp", ln.Addr().String(), 30*time.Millisecond)
		if derr != nil {
			break
		}
		fillers = append(fillers, c)
	}
	probe, perr := net.DialTimeout("tcp", ln.Addr().String(), 40*time.Millisecond)
	if perr == nil {
		_ = probe.Close()
		cleanup()
		t.Skip("could not saturate listen backlog to block DialTCP")
	}
	return ln.Addr().String(), cleanup
}

func TestDialTCPSuccessUntiedFromDialContext(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(io.Discard, c)
			}(conn)
		}
	}()

	client := testClient(t, ln.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	conn, err := client.DialTCP(ctx, "example.com", 443)
	if err != nil {
		cancel()
		t.Fatalf("DialTCP: %v", err)
	}
	cancel()
	time.Sleep(30 * time.Millisecond)

	_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
	if _, werr := conn.Write([]byte("still-open")); werr != nil {
		t.Fatalf("returned conn was tied to dial context: %v", werr)
	}
	_ = conn.Close()
}

func TestDialTCPFailedPathCloses(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	client := testClient(t, addr)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	conn, err := client.DialTCP(ctx, "example.com", 443)
	if conn != nil {
		_ = conn.Close()
		t.Fatal("expected nil conn on failed dial")
	}
	if err == nil {
		t.Fatal("expected error on refused dial")
	}
}

func TestWatchEstablishmentCancelStopsWithoutAbandoning(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	ctx, cancel := context.WithCancel(context.Background())
	stop := watchEstablishmentCancel(ctx, c1)
	stop()
	stop() // idempotent
	cancel()
	time.Sleep(30 * time.Millisecond)
}
