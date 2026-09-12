package proxy

import (
	"context"
	"io"
	"net"
	"sync"
	"time"

	"github.com/missuo/opensnell/components/snell"
)

type Dialer interface {
	DialTCP(ctx context.Context, host string, port uint16) (net.Conn, error)
	Close() error
	ID() string
}

type Adapter struct {
	id     string
	client *snell.Client
	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	closed bool
}

func NewAdapter(spec NodeSpec) (*Adapter, error) {
	if spec.Version != "v4" && spec.Version != "v5" {
		return nil, errCode("unsupported_version")
	}
	cfg := snell.ClientConfig{
		Server:      spec.ServerAddr(),
		PSK:         spec.PSK,
		ObfsMode:    spec.ObfsMode,
		ObfsHost:    spec.ObfsHost,
		Reuse:       false,
		TFO:         false,
		Version:     spec.Version,
		DialTimeout: 8 * time.Second,
	}
	RememberSecrets(&Snapshot{Nodes: []NodeSpec{spec}}, "")
	c, err := snell.NewClient(cfg, RedactingLogger(io.Discard))
	if err != nil {
		return nil, Sanitize(err)
	}
	return &Adapter{
		id:     spec.ID,
		client: c,
		conns:  make(map[net.Conn]struct{}),
	}, nil
}

func (a *Adapter) ID() string { return a.id }

func (a *Adapter) DialTCP(ctx context.Context, host string, port uint16) (net.Conn, error) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil, errCode("retired")
	}
	a.mu.Unlock()

	c, err := a.client.DialTCP(ctx, host, port)
	if err != nil {
		return nil, Sanitize(err)
	}
	wrapped := &ownedConn{Conn: c, adapter: a}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		_ = wrapped.Conn.Close()
		return nil, errCode("retired")
	}
	a.conns[wrapped] = struct{}{}
	a.mu.Unlock()
	return wrapped, nil
}

func (a *Adapter) Close() error {
	a.mu.Lock()
	a.closed = true
	conns := make([]net.Conn, 0, len(a.conns))
	for c := range a.conns {
		conns = append(conns, c)
	}
	a.conns = map[net.Conn]struct{}{}
	a.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
	return nil
}

func (a *Adapter) forget(c net.Conn) {
	a.mu.Lock()
	delete(a.conns, c)
	a.mu.Unlock()
}

type ownedConn struct {
	net.Conn
	adapter *Adapter
	once    sync.Once
}

func (c *ownedConn) Close() error {
	var err error
	c.once.Do(func() {
		err = c.Conn.Close()
		c.adapter.forget(c)
	})
	return err
}
