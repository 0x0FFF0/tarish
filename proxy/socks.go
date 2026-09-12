package proxy

import (
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type SOCKSServer struct {
	Target   Target
	Dial     func(host string, port int) (net.Conn, error)
	Clock    Clock
	MaxConns int
	ln       net.Listener
	mu       sync.Mutex
	active   int32
	closed   bool
}

func (s *SOCKSServer) Listen(bind string) (net.Listener, error) {
	if bind == "" {
		bind = "127.0.0.1:0"
	}
	host, _, err := net.SplitHostPort(bind)
	if err != nil {
		return nil, errCode("listen_failed")
	}
	if host != "127.0.0.1" && host != "localhost" {
		return nil, errCode("bind_not_loopback")
	}
	ln, err := net.Listen("tcp", bind)
	if err != nil {
		return nil, errCode("listen_failed")
	}
	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok || tcpAddr.IP == nil || !tcpAddr.IP.IsLoopback() {
		_ = ln.Close()
		return nil, errCode("bind_not_loopback")
	}
	s.ln = ln
	return ln, nil
}

func (s *SOCKSServer) Addr() string {
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

func (s *SOCKSServer) Serve() error {
	if s.MaxConns <= 0 {
		s.MaxConns = MaxSOCKSConns
	}
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return nil
			}
			return Sanitize(err)
		}
		if atomic.LoadInt32(&s.active) >= int32(s.MaxConns) {
			_ = conn.Close()
			continue
		}
		atomic.AddInt32(&s.active, 1)
		go func(c net.Conn) {
			defer atomic.AddInt32(&s.active, -1)
			s.handle(c)
		}(conn)
	}
}

func (s *SOCKSServer) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	if s.ln != nil {
		return s.ln.Close()
	}
	return nil
}

func (s *SOCKSServer) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(HandshakeTimeout))

	head := make([]byte, 2)
	if _, err := io.ReadFull(conn, head); err != nil {
		return
	}
	if head[0] != 0x05 {
		return
	}
	nmethods := int(head[1])
	if nmethods < 0 {
		return
	}
	methods := make([]byte, nmethods)
	if _, err := io.ReadFull(conn, methods); err != nil {
		return
	}
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	hdr := make([]byte, 4)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		return
	}
	if hdr[0] != 0x05 {
		return
	}
	cmd := hdr[1]
	atyp := hdr[3]
	host, port, err := readSOCKSAddr(conn, atyp)
	if err != nil {
		writeSOCKSReply(conn, 0x08)
		return
	}
	switch cmd {
	case 0x01: // CONNECT
	case 0x02, 0x03: // BIND, UDP ASSOCIATE
		writeSOCKSReply(conn, 0x07)
		return
	default:
		writeSOCKSReply(conn, 0x07)
		return
	}

	host, port, err = CanonicalHostPort(host, port)
	if err != nil {
		writeSOCKSReply(conn, 0x08)
		return
	}
	wantHost, wantPort, err := CanonicalHostPort(s.Target.Host, s.Target.Port)
	if err != nil || host != wantHost || port != wantPort {
		writeSOCKSReply(conn, 0x02)
		return
	}

	_ = conn.SetDeadline(time.Time{})
	up, err := s.Dial(host, port)
	if err != nil {
		writeSOCKSReply(conn, 0x05)
		return
	}
	defer up.Close()
	if err := writeSOCKSReply(conn, 0x00); err != nil {
		return
	}
	relay(conn, up)
}

func readSOCKSAddr(r io.Reader, atyp byte) (string, int, error) {
	switch atyp {
	case 0x01:
		buf := make([]byte, 4+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return "", 0, err
		}
		host := net.IP(buf[:4]).String()
		port := int(binary.BigEndian.Uint16(buf[4:]))
		return host, port, nil
	case 0x04:
		buf := make([]byte, 16+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return "", 0, err
		}
		host := net.IP(buf[:16]).String()
		port := int(binary.BigEndian.Uint16(buf[16:]))
		return host, port, nil
	case 0x03:
		l := make([]byte, 1)
		if _, err := io.ReadFull(r, l); err != nil {
			return "", 0, err
		}
		buf := make([]byte, int(l[0])+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return "", 0, err
		}
		host := string(buf[:len(buf)-2])
		port := int(binary.BigEndian.Uint16(buf[len(buf)-2:]))
		return host, port, nil
	default:
		return "", 0, errCode("address_not_supported")
	}
}

func writeSOCKSReply(w io.Writer, rep byte) error {
	_, err := w.Write([]byte{0x05, rep, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	return err
}

func relay(a, b net.Conn) {
	done := make(chan struct{}, 2)
	copy := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		_ = dst.SetReadDeadline(time.Now())
		done <- struct{}{}
	}
	go copy(a, b)
	go copy(b, a)
	<-done
	_ = a.Close()
	_ = b.Close()
	<-done
}

func SOCKSConnect(conn net.Conn, host string, port int) error {
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return err
	}
	ack := make([]byte, 2)
	if _, err := io.ReadFull(conn, ack); err != nil {
		return err
	}
	if ack[0] != 0x05 || ack[1] != 0x00 {
		return errCode("socks_auth")
	}
	req := []byte{0x05, 0x01, 0x00}
	ip := net.ParseIP(host)
	switch {
	case ip != nil && ip.To4() != nil:
		req = append(req, 0x01)
		req = append(req, ip.To4()...)
	case ip != nil:
		req = append(req, 0x04)
		req = append(req, ip.To16()...)
	default:
		if len(host) > 255 {
			return errCode("invalid_target")
		}
		req = append(req, 0x03, byte(len(host)))
		req = append(req, host...)
	}
	var pb [2]byte
	binary.BigEndian.PutUint16(pb[:], uint16(port))
	req = append(req, pb[:]...)
	if _, err := conn.Write(req); err != nil {
		return err
	}
	rep := make([]byte, 4)
	if _, err := io.ReadFull(conn, rep); err != nil {
		return err
	}
	if rep[1] != 0x00 {
		return errCode("socks_rejected")
	}
	switch rep[3] {
	case 0x01:
		_, err := io.ReadFull(conn, make([]byte, 4+2))
		return err
	case 0x04:
		_, err := io.ReadFull(conn, make([]byte, 16+2))
		return err
	case 0x03:
		l := make([]byte, 1)
		if _, err := io.ReadFull(conn, l); err != nil {
			return err
		}
		_, err := io.ReadFull(conn, make([]byte, int(l[0])+2))
		return err
	default:
		return errCode("socks_rejected")
	}
}

func ParsePort(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
