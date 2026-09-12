package proxy

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"tarish/snellspec"
)

type Fetcher struct {
	Client    *http.Client
	Store     *Store
	Clock     Clock
	Format    Format
	nowRand   func() float64
	Bootstrap func() (Dialer, error)
	TLSConfig *tls.Config
}

func IndependentHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = FetchTimeout
	}
	t := &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		DisableKeepAlives:     false,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: t,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errCode("too_many_redirects")
			}
			if req.URL.Scheme != "https" {
				return errCode("https_downgrade")
			}
			orig := via[0].URL
			if !sameOrigin(orig, req.URL) {
				return errCode("redirect_escape")
			}
			return nil
		},
	}
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

func NewFetcher(store *Store, format Format) *Fetcher {
	return &Fetcher{
		Client:  IndependentHTTPClient(FetchTimeout),
		Store:   store,
		Clock:   realClock{},
		Format:  format,
		nowRand: jitterUnit,
		Bootstrap: func() (Dialer, error) {
			return NewAdapter(BootstrapNode())
		},
	}
}

func HTTPClientWithDialer(d Dialer, timeout time.Duration, tlsCfg *tls.Config) *http.Client {
	c := IndependentHTTPClient(timeout)
	base, _ := c.Transport.(*http.Transport)
	t := base.Clone()
	t.Proxy = nil
	if tlsCfg != nil {
		t.TLSClientConfig = tlsCfg
	}
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, portStr, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, errCode("invalid_target")
		}
		p, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, errCode("invalid_target")
		}
		return d.DialTCP(ctx, host, uint16(p))
	}
	c.Transport = t
	return c
}

func jitterUnit() float64 {
	n, err := rand.Int(rand.Reader, big.NewInt(1<<53))
	if err != nil {
		var b [8]byte
		_, _ = rand.Read(b[:])
		return float64(binary.BigEndian.Uint64(b[:])>>11) / float64(1<<53)
	}
	return float64(n.Int64()) / float64(1<<53)
}

func (f *Fetcher) JitteredInterval() time.Duration {
	u := 0.5
	if f.nowRand != nil {
		u = f.nowRand()
	}
	j := (u*2 - 1) * 0.10
	return time.Duration(float64(RefreshInterval) * (1 + j))
}

func (f *Fetcher) Refresh(ctx context.Context) (*Snapshot, error) {
	ps, err := f.Store.Load()
	if err != nil {
		return nil, err
	}
	if ps == nil {
		return nil, errCode("not_configured")
	}
	if ps.StaticDocument {
		if ps.Snapshot == nil {
			return nil, errCode("not_configured")
		}
		now := f.Clock.Now()
		snap := mergeBootstrap(ps.Snapshot.toSnapshot())
		snap.FetchedAt = now
		snap.ValidatedAt = now
		ps.FetchedAt = now
		ps.ValidatedAt = now
		ps.LastErrorCode = ""
		ps.Snapshot = snapshotPersist(snap)
		if err := f.Store.Save(ps); err != nil {
			return nil, err
		}
		_ = f.Store.SaveCache(ps)
		return snap, nil
	}
	if strings.TrimSpace(ps.SubscriptionURL) == "" {
		return nil, errCode("not_configured")
	}
	if !strings.HasPrefix(strings.ToLower(ps.SubscriptionURL), "https://") {
		return nil, errCode("https_required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ps.SubscriptionURL, nil)
	if err != nil {
		return nil, errCode("fetch_failed")
	}
	if ps.ETag != "" {
		req.Header.Set("If-None-Match", ps.ETag)
	}
	if ps.LastModified != "" {
		req.Header.Set("If-Modified-Since", ps.LastModified)
	}

	resp, err := f.fetch(ctx, req)
	if err != nil {
		return f.keepLastGood(ps, errCode("fetch_failed"))
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		now := f.Clock.Now()
		ps.ValidatedAt = now
		ps.LastErrorCode = ""
		ps.Snapshot = snapshotPersist(mergeBootstrap(ps.Snapshot.toSnapshot()))
		if err := f.Store.Save(ps); err != nil {
			return f.keepLastGood(ps, err)
		}
		_ = f.Store.SaveCache(ps)
		snap := ps.Snapshot.toSnapshot()
		if snap != nil {
			snap.ValidatedAt = now
			snap.FetchedAt = ps.FetchedAt
			snap.ETag = ps.ETag
			snap.LastModified = ps.LastModified
		}
		return snap, nil
	}

	limited := io.LimitReader(resp.Body, MaxDocumentBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return f.keepLastGood(ps, errCode("fetch_failed"))
	}
	if len(body) > MaxDocumentBytes {
		return f.keepLastGood(ps, errCode("document_too_large"))
	}

	prefix := ""
	if len(body) > 64 {
		prefix = string(body[:64])
	} else {
		prefix = string(body)
	}
	if err := ClassifyFetch(resp.StatusCode, resp.Header.Get("Content-Type"), prefix); err != nil {
		return f.keepLastGood(ps, err)
	}

	format := f.Format
	if format == "" {
		format = Format(ps.Format)
	}
	snap, err := ParseDocument(body, format)
	if err != nil {
		return f.keepLastGood(ps, err)
	}

	now := f.Clock.Now()
	assignStableIDs(ps.Snapshot.toSnapshot(), snap)
	snap = mergeBootstrap(snap)
	snap.FetchedAt = now
	snap.ValidatedAt = now
	snap.ETag = resp.Header.Get("ETag")
	snap.LastModified = resp.Header.Get("Last-Modified")

	ps.Snapshot = snapshotPersist(snap)
	ps.ETag = snap.ETag
	ps.LastModified = snap.LastModified
	ps.FetchedAt = now
	ps.ValidatedAt = now
	ps.LastErrorCode = ""
	if err := f.Store.Save(ps); err != nil {
		return f.keepLastGood(ps, err)
	}
	if err := f.Store.SaveCache(ps); err != nil {
		return snap, Sanitize(err)
	}
	return snap, nil
}

func (f *Fetcher) fetch(ctx context.Context, req *http.Request) (*http.Response, error) {
	resp, err := f.Client.Do(req.Clone(ctx))
	if err == nil {
		return resp, nil
	}
	if ctx.Err() != nil || !shouldFetchViaBootstrap(err) {
		return nil, err
	}
	return f.fetchViaBootstrap(ctx, req)
}

func shouldFetchViaBootstrap(err error) bool {
	code := ErrorCode(err)
	switch code {
	case "redirect_escape", "https_downgrade", "too_many_redirects", "canceled":
		return false
	default:
		return true
	}
}

func (f *Fetcher) fetchViaBootstrap(ctx context.Context, req *http.Request) (*http.Response, error) {
	makeDialer := f.Bootstrap
	if makeDialer == nil {
		return nil, errCode("fetch_failed")
	}
	d, err := makeDialer()
	if err != nil {
		return nil, Sanitize(err)
	}
	defer d.Close()
	client := HTTPClientWithDialer(d, FetchTimeout, f.TLSConfig)
	resp, err := client.Do(req.Clone(ctx))
	if err != nil {
		return nil, Sanitize(err)
	}
	return resp, nil
}

func (f *Fetcher) keepLastGood(ps *persistedStore, cause error) (*Snapshot, error) {
	code := ErrorCode(cause)
	if ps != nil {
		ps.LastErrorCode = code
		if ps.Snapshot != nil && len(ps.Snapshot.Nodes) > 0 {
			snap := mergeBootstrap(ps.Snapshot.toSnapshot())
			ps.Snapshot = snapshotPersist(snap)
			_ = f.Store.Save(ps)
			snap.FetchedAt = ps.FetchedAt
			snap.ValidatedAt = ps.ValidatedAt
			return snap, errCode(code)
		}
		now := f.Clock.Now()
		if now.IsZero() {
			now = time.Now()
		}
		snap := mergeBootstrap(nil)
		snap.FetchedAt = now
		snap.ValidatedAt = now
		ps.Snapshot = snapshotPersist(snap)
		if ps.ValidatedAt.IsZero() {
			ps.ValidatedAt = now
			ps.FetchedAt = now
		}
		_ = f.Store.Save(ps)
		return snap, errCode(code)
	}
	return nil, errCode(code)
}

func assignStableIDs(old, next *Snapshot) {
	snellspec.AssignStableIDs(old, next)
}

func UsableSnapshot(ps *persistedStore, now time.Time) (*Snapshot, bool, bool, error) {
	if ps == nil || ps.Snapshot == nil || len(ps.Snapshot.Nodes) == 0 {
		return nil, false, true, errCode("not_configured")
	}
	stale, expired, _ := CacheFreshness(ps.ValidatedAt, now)
	snap := ps.Snapshot.toSnapshot()
	snap.FetchedAt = ps.FetchedAt
	snap.ValidatedAt = ps.ValidatedAt
	snap.ETag = ps.ETag
	snap.LastModified = ps.LastModified
	if expired {
		return snap, stale, true, errCode("cache_expired")
	}
	return snap, stale, false, nil
}
