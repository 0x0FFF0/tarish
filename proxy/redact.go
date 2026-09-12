package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"

	"tarish/snellspec"
)

type codedError struct {
	code string
}

func (e *codedError) Error() string { return e.code }

func errCode(code string) error {
	return &codedError{code: code}
}

func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var ce *codedError
	if errors.As(err, &ce) {
		return ce.code
	}
	if c := snellspec.CodeOf(err); c != "" {
		return c
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	return "transport_error"
}

func Sanitize(err error) error {
	if err == nil {
		return nil
	}
	var ce *codedError
	if errors.As(err, &ce) {
		return ce
	}
	return errCode(ErrorCode(err))
}

type secretBag struct {
	mu      sync.RWMutex
	secrets []string
}

func (b *secretBag) Set(values ...string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.secrets = b.secrets[:0]
	for _, v := range values {
		v = strings.TrimSpace(v)
		if len(v) >= 4 {
			b.secrets = append(b.secrets, v)
		}
	}
}

func (b *secretBag) AddSnapshot(s *Snapshot, url string) {
	vals := []string{url}
	if s != nil {
		for _, n := range s.Nodes {
			vals = append(vals, n.PSK, n.Host, n.Name, n.ObfsHost, n.ServerAddr())
		}
	}
	b.Set(vals...)
}

func (b *secretBag) Redact(s string) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := s
	for _, sec := range b.secrets {
		if sec != "" && strings.Contains(out, sec) {
			out = strings.ReplaceAll(out, sec, "[redacted]")
		}
	}
	return out
}

func (b *secretBag) Contains(s string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, sec := range b.secrets {
		if sec != "" && strings.Contains(s, sec) {
			return true
		}
	}
	return false
}

var secrets secretBag

func RedactString(s string) string { return secrets.Redact(s) }

func RememberSecrets(s *Snapshot, url string) { secrets.AddSnapshot(s, url) }

type redactHandler struct {
	inner slog.Handler
}

func (h redactHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h redactHandler) Handle(ctx context.Context, r slog.Record) error {
	r.Message = secrets.Redact(r.Message)
	var attrs []slog.Attr
	r.Attrs(func(a slog.Attr) bool {
		a.Value = slog.StringValue(secrets.Redact(a.Value.String()))
		attrs = append(attrs, a)
		return true
	})
	nr := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	nr.AddAttrs(attrs...)
	return h.inner.Handle(ctx, nr)
}

func (h redactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return redactHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h redactHandler) WithGroup(name string) slog.Handler {
	return redactHandler{inner: h.inner.WithGroup(name)}
}

func RedactingLogger(w io.Writer) *slog.Logger {
	if w == nil {
		w = io.Discard
	}
	return slog.New(redactHandler{inner: slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo})})
}

func SafeErrorf(code string, _ ...any) error {
	return errCode(code)
}

func ClassifyFetch(status int, contentType, bodyPrefix string) error {
	ct := strings.ToLower(contentType)
	p := strings.ToLower(strings.TrimSpace(bodyPrefix))
	if strings.Contains(ct, "text/html") || strings.HasPrefix(p, "<!doctype html") || strings.HasPrefix(p, "<html") {
		return errCode("html_document")
	}
	if status == 304 {
		return nil
	}
	if status < 200 || status >= 300 {
		return errCode(fmt.Sprintf("http_%d", status))
	}
	return nil
}
