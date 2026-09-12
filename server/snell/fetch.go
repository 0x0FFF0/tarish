package snell

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"tarish/snellspec"
)

const (
	FetchTimeout  = 15 * time.Second
	MaxRedirects  = 3
	RefreshBase   = 30 * time.Minute
	RefreshJitter = 0.10
)

type Limits struct {
	Timeout       time.Duration
	MaxBytes      int64
	MaxRedirects  int
	AllowLoopback bool
	TLSConfig     *tls.Config
}

type Fetcher struct {
	Limits Limits
	Client *http.Client
}

type Result struct {
	Body         []byte
	Status       int
	ETag         string
	LastModified string
	ContentType  string
	NotModified  bool
}

func NewFetcher(lim Limits) *Fetcher {
	if lim.Timeout <= 0 {
		lim.Timeout = FetchTimeout
	}
	if lim.MaxBytes <= 0 {
		lim.MaxBytes = snellspec.MaxDocumentBytes
	}
	if lim.MaxRedirects <= 0 {
		lim.MaxRedirects = MaxRedirects
	}
	f := &Fetcher{Limits: lim}
	f.Client = f.buildClient()
	return f
}

func (f *Fetcher) buildClient() *http.Client {
	lim := f.Limits
	dialer := &net.Dialer{
		Timeout: 15 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				host = address
			}
			if ip := net.ParseIP(host); ip != nil && blockedIP(ip, lim.AllowLoopback) {
				return snellspec.Err("blocked_destination")
			}
			return nil
		},
	}
	transport := &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: lim.Timeout,
		TLSClientConfig:       lim.TLSConfig,
		DialContext:           dialer.DialContext,
	}
	return &http.Client{
		Timeout:   lim.Timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= lim.MaxRedirects {
				return snellspec.Err("too_many_redirects")
			}
			if req.URL.Scheme != "https" {
				return snellspec.Err("https_downgrade")
			}
			orig := via[0].URL
			if !sameOrigin(orig, req.URL) {
				return snellspec.Err("redirect_escape")
			}
			if err := f.checkURL(req.URL); err != nil {
				return err
			}
			return nil
		},
	}
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

func (f *Fetcher) checkURL(u *url.URL) error {
	if u == nil {
		return snellspec.Err("https_required")
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return snellspec.Err("https_required")
	}
	host := u.Hostname()
	if host == "" {
		return snellspec.Err("invalid_target")
	}
	if ip := net.ParseIP(host); ip != nil {
		if blockedIP(ip, f.Limits.AllowLoopback) {
			return snellspec.Err("blocked_destination")
		}
	}
	return nil
}

func (f *Fetcher) checkDestination(ctx context.Context, host string) error {
	if ip := net.ParseIP(host); ip != nil {
		if blockedIP(ip, f.Limits.AllowLoopback) {
			return snellspec.Err("blocked_destination")
		}
		return nil
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return snellspec.Err("fetch_failed")
	}
	ok := false
	for _, ip := range ips {
		if !blockedIP(ip.IP, f.Limits.AllowLoopback) {
			ok = true
			break
		}
	}
	if !ok {
		return snellspec.Err("blocked_destination")
	}
	return nil
}

func blockedIP(ip net.IP, allowLoopback bool) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() {
		return !allowLoopback
	}
	return ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast()
}

func (f *Fetcher) Get(ctx context.Context, rawURL, etag, lastModified string) (*Result, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, snellspec.Err("https_required")
	}
	if err := f.checkURL(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, snellspec.Err("fetch_failed")
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}
	if !f.Limits.AllowLoopback {
		cctx, cancel := context.WithTimeout(context.Background(), FetchTimeout)
		alt, cerr := curlHTTPS(cctx, u.String(), etag, lastModified)
		cancel()
		if cerr == nil {
			return alt, nil
		}
		log.Printf("[snell] curl fetch: %s", snellspec.CodeOf(cerr))
	}
	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, mapFetchErr(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return &Result{
			Status:       resp.StatusCode,
			ETag:         resp.Header.Get("ETag"),
			LastModified: resp.Header.Get("Last-Modified"),
			ContentType:  resp.Header.Get("Content-Type"),
			NotModified:  true,
		}, nil
	}
	limited := io.LimitReader(resp.Body, f.Limits.MaxBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, snellspec.Err("fetch_failed")
	}
	if int64(len(body)) > f.Limits.MaxBytes {
		return nil, snellspec.Err("document_too_large")
	}
	prefix := ""
	if len(body) > 64 {
		prefix = string(body[:64])
	} else {
		prefix = string(body)
	}
	if err := classify(resp.StatusCode, resp.Header.Get("Content-Type"), prefix); err != nil {
		return nil, err
	}
	return &Result{
		Body:         body,
		Status:       resp.StatusCode,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		ContentType:  resp.Header.Get("Content-Type"),
	}, nil
}

func curlHTTPS(ctx context.Context, rawURL, etag, lastModified string) (*Result, error) {
	if _, err := exec.LookPath("curl"); err != nil {
		return nil, snellspec.Err("fetch_failed")
	}
	dir, err := os.MkdirTemp("", "snell-curl-*")
	if err != nil {
		return nil, snellspec.Err("fetch_failed")
	}
	defer os.RemoveAll(dir)
	bodyPath := filepath.Join(dir, "body")
	hdrPath := filepath.Join(dir, "hdr")
	args := []string{
		"-sS", "-D", hdrPath, "-o", bodyPath,
		"--max-time", "15",
		"--max-redirs", strconv.Itoa(MaxRedirects),
		"--proto", "=https",
		"--proto-redir", "=https",
		"--tlsv1.2",
	}
	if etag != "" {
		args = append(args, "-H", "If-None-Match: "+etag)
	}
	if lastModified != "" {
		args = append(args, "-H", "If-Modified-Since: "+lastModified)
	}
	args = append(args, rawURL)
	cmd := exec.CommandContext(ctx, "curl", args...)
	if err := cmd.Run(); err != nil {
		return nil, snellspec.Err("fetch_failed")
	}
	hdr, err := os.ReadFile(hdrPath)
	if err != nil {
		return nil, snellspec.Err("fetch_failed")
	}
	body, err := os.ReadFile(bodyPath)
	if err != nil {
		return nil, snellspec.Err("fetch_failed")
	}
	status, ct, et, lm := parseCurlHeaders(hdr)
	if status == http.StatusNotModified {
		return &Result{Status: status, ContentType: ct, ETag: et, LastModified: lm, NotModified: true}, nil
	}
	if err := classify(status, ct, string(body)); err != nil {
		return nil, err
	}
	return &Result{Body: body, Status: status, ContentType: ct, ETag: et, LastModified: lm}, nil
}

func parseCurlHeaders(raw []byte) (status int, ct, etag, lastMod string) {
	for _, block := range strings.Split(string(raw), "\r\n\r\n") {
		lines := strings.Split(block, "\n")
		if len(lines) == 0 {
			continue
		}
		first := strings.TrimSpace(lines[0])
		if !strings.HasPrefix(first, "HTTP/") {
			continue
		}
		parts := strings.Fields(first)
		if len(parts) >= 2 {
			if n, err := strconv.Atoi(parts[1]); err == nil {
				status = n
			}
		}
		for _, line := range lines[1:] {
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			key := strings.TrimSpace(k)
			val := strings.TrimSpace(v)
			switch {
			case strings.EqualFold(key, "Content-Type"):
				ct = val
			case strings.EqualFold(key, "ETag"):
				etag = val
			case strings.EqualFold(key, "Last-Modified"):
				lastMod = val
			}
		}
	}
	if status == 0 {
		status = 200
	}
	return status, ct, etag, lastMod
}

func mapFetchErr(err error) error {
	if c := snellspec.CodeOf(err); c != "" {
		return err
	}
	var ue *url.Error
	inner := err
	if errors.As(err, &ue) {
		inner = ue.Err
		if c := snellspec.CodeOf(ue.Err); c != "" {
			return ue.Err
		}
	}
	log.Printf("[snell] fetch transport: %s", inner)
	switch {
	case errors.Is(inner, context.DeadlineExceeded), errors.Is(inner, context.Canceled):
		return snellspec.Err("timeout")
	}
	var ne net.Error
	if errors.As(inner, &ne) && ne.Timeout() {
		return snellspec.Err("timeout")
	}
	var unknown x509.UnknownAuthorityError
	if errors.As(inner, &unknown) {
		return snellspec.Err("tls_verify")
	}
	var hostname x509.HostnameError
	if errors.As(inner, &hostname) {
		return snellspec.Err("tls_verify")
	}
	return snellspec.Err("fetch_failed")
}

func classify(status int, contentType, bodyPrefix string) error {
	ct := strings.ToLower(contentType)
	p := strings.ToLower(strings.TrimSpace(bodyPrefix))
	if strings.Contains(ct, "text/html") || strings.HasPrefix(p, "<!doctype html") || strings.HasPrefix(p, "<html") {
		return snellspec.Err("html_document")
	}
	if status < 200 || status >= 300 {
		return snellspec.Err(fmt.Sprintf("http_%d", status))
	}
	return nil
}

func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "[redacted]"
	}
	out := u.Scheme + "://" + u.Host + u.Path
	if u.RawQuery != "" || u.User != nil {
		out += "?[redacted]"
	}
	return out
}
