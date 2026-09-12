package proxy

import (
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestSOCKSAllowlistAndCommands(t *testing.T) {
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
	_, port, _ := SplitHostPort(ln.Addr().String())
	target := Target{Host: "127.0.0.1", Port: port}

	s := &SOCKSServer{
		Target: target,
		Dial: func(host string, p int) (net.Conn, error) {
			if host != "127.0.0.1" || p != port {
				t.Errorf("dialed %s:%d", host, p)
				return nil, errCode("invalid_target")
			}
			return net.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(p)))
		},
	}
	if _, err := s.Listen("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	go s.Serve()
	time.Sleep(20 * time.Millisecond)

	c, err := net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	if err := SOCKSConnect(c, "127.0.0.1", port); err != nil {
		t.Fatalf("allowlisted connect: %v", err)
	}
	_ = c.Close()

	c, err = net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	if err := SOCKSConnect(c, "example.com", port); err == nil {
		t.Fatal("hostname that is not the configured target was accepted")
	}
	_ = c.Close()

	c, err = net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c.Write([]byte{0x05, 0x01, 0x00})
	ack := make([]byte, 2)
	_, _ = io.ReadFull(c, ack)
	_, _ = c.Write([]byte{0x05, 0x02, 0x00, 0x01, 127, 0, 0, 1, 0, 1})
	rep := make([]byte, 2)
	_, _ = io.ReadFull(c, rep)
	if rep[1] != 0x07 {
		t.Fatalf("BIND reply %x want 07", rep[1])
	}
	_ = c.Close()

	if _, err := (&SOCKSServer{}).Listen("0.0.0.0:0"); ErrorCode(err) != "bind_not_loopback" {
		t.Fatalf("LAN bind: %v", err)
	}
}
