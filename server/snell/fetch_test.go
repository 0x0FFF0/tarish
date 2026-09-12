package snell

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"tarish/snellspec"
)

func TestFetchTooManySameOriginRedirects(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/next", http.StatusFound)
	}))
	defer srv.Close()
	f := NewFetcher(Limits{
		AllowLoopback: true,
		TLSConfig:     &tls.Config{InsecureSkipVerify: true},
		Timeout:       3 * time.Second,
		MaxRedirects:  3,
	})
	_, err := f.Get(t.Context(), srv.URL, "", "")
	if snellspec.CodeOf(err) != "too_many_redirects" && snellspec.CodeOf(err) != "fetch_failed" {
		t.Fatalf("got %v", err)
	}
}

func TestFetchHTTPSRequiredAndLoopbackBlocked(t *testing.T) {
	f := NewFetcher(Limits{Timeout: 2 * time.Second})
	_, err := f.Get(t.Context(), "http://example.com/", "", "")
	if snellspec.CodeOf(err) != "https_required" {
		t.Fatalf("http %v", err)
	}
	_, err = f.Get(t.Context(), "https://127.0.0.1/", "", "")
	if snellspec.CodeOf(err) != "blocked_destination" {
		t.Fatalf("loopback %v", err)
	}
}
