package update

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMain(m *testing.M) {
	// Tests must never hit production. Each test points baseURL at httptest.
	baseURL = "http://127.0.0.1:1/tarish-tests-must-override-baseurl"
	os.Exit(m.Run())
}

type fakeUpdateServer struct {
	version    atomic.Value // string
	status     atomic.Int32 // 0 or 200 = 200
	binaryGETs atomic.Int32
	srv        *httptest.Server
}

func newFakeUpdateServer(t *testing.T) *fakeUpdateServer {
	t.Helper()
	f := &fakeUpdateServer{}
	f.version.Store("v1.1.1")
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/version" || r.URL.Path == "/version/":
			if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			if code := f.status.Load(); code != 0 && code != http.StatusOK {
				w.WriteHeader(int(code))
				return
			}
			io.WriteString(w, f.version.Load().(string))
		case strings.HasPrefix(r.URL.Path, "/dist/"):
			if r.Method == http.MethodGet {
				f.binaryGETs.Add(1)
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			io.WriteString(w, "fake-tarish-binary")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeUpdateServer) setVersion(body string) {
	f.version.Store(body)
}

func (f *fakeUpdateServer) setStatus(code int) {
	f.status.Store(int32(code))
}

func setupUpdateTest(t *testing.T) *fakeUpdateServer {
	t.Helper()
	home := t.TempDir()
	t.Setenv("TARISH_HOME", home)
	t.Setenv("TARISH_USER", "tester")
	t.Setenv("SUDO_USER", "")

	origBase := baseURL
	origReplace := replaceBinaryFn
	origVer := Version
	origAfter := AfterApply
	t.Cleanup(func() {
		baseURL = origBase
		replaceBinaryFn = origReplace
		Version = origVer
		AfterApply = origAfter
	})

	replaceBinaryFn = func(string) error {
		return errors.New("forced apply failure")
	}
	AfterApply = nil

	f := newFakeUpdateServer(t)
	baseURL = f.srv.URL
	return f
}

func TestAutoUpdateSkipsBinaryWhenAdvertisedMatchesRunning(t *testing.T) {
	f := setupUpdateTest(t)
	Version = "v1.1.1"
	f.setVersion("  v1.1.1\n")

	if got := AutoUpdate(); got != AutoUpdateNoChange {
		t.Fatalf("AutoUpdate() = %v, want AutoUpdateNoChange", got)
	}
	if n := f.binaryGETs.Load(); n != 0 {
		t.Fatalf("binary GETs = %d, want 0 when advertised version matches", n)
	}
}

func TestUpdateSkipsBinaryWhenAdvertisedMatchesRunning(t *testing.T) {
	f := setupUpdateTest(t)
	Version = "v1.1.1"
	f.setVersion("\tv1.1.1  \n")

	if err := Update(); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if n := f.binaryGETs.Load(); n != 0 {
		t.Fatalf("binary GETs = %d, want 0 when advertised version matches", n)
	}
}

func TestAutoUpdateSkipsBinaryWhenVersionProbeFails(t *testing.T) {
	t.Run("non-200", func(t *testing.T) {
		f := setupUpdateTest(t)
		Version = "v1.0.0"
		f.setStatus(http.StatusInternalServerError)

		if got := AutoUpdate(); got != AutoUpdateCheckErr {
			t.Fatalf("AutoUpdate() = %v, want AutoUpdateCheckErr", got)
		}
		if n := f.binaryGETs.Load(); n != 0 {
			t.Fatalf("binary GETs = %d, want 0 on version probe error", n)
		}
	})

	t.Run("network error", func(t *testing.T) {
		f := setupUpdateTest(t)
		Version = "v1.0.0"
		f.srv.Close()

		if got := AutoUpdate(); got != AutoUpdateCheckErr {
			t.Fatalf("AutoUpdate() = %v, want AutoUpdateCheckErr", got)
		}
		if n := f.binaryGETs.Load(); n != 0 {
			t.Fatalf("binary GETs = %d, want 0 on version probe error", n)
		}
	})
}

func TestAutoUpdateSkipsBinaryWhenVersionBodyUnusable(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "empty", body: ""},
		{name: "whitespace", body: "  \n\t"},
		{name: "html", body: "<html>index</html>"},
		{name: "garbage", body: "not a version!!!"},
		{name: "multi-token", body: "v1.1.1 extra"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := setupUpdateTest(t)
			Version = "v1.0.0"
			f.setVersion(tc.body)

			if got := AutoUpdate(); got != AutoUpdateCheckErr {
				t.Fatalf("AutoUpdate() = %v, want AutoUpdateCheckErr for body %q", got, tc.body)
			}
			if n := f.binaryGETs.Load(); n != 0 {
				t.Fatalf("binary GETs = %d, want 0 for unusable version body %q", n, tc.body)
			}
		})
	}
}

func TestAutoUpdateDownloadsOncePerAdvertisedVersionAfterFailedApply(t *testing.T) {
	f := setupUpdateTest(t)
	Version = "v1.0.0"
	f.setVersion("v1.1.1")

	if got := AutoUpdate(); got != AutoUpdateFailed {
		t.Fatalf("first AutoUpdate() = %v, want AutoUpdateFailed", got)
	}
	if n := f.binaryGETs.Load(); n != 1 {
		t.Fatalf("binary GETs after first AutoUpdate = %d, want 1", n)
	}

	if got := AutoUpdate(); got != AutoUpdateNoChange {
		t.Fatalf("second AutoUpdate() = %v, want AutoUpdateNoChange (same advertised version)", got)
	}
	if n := f.binaryGETs.Load(); n != 1 {
		t.Fatalf("binary GETs after second AutoUpdate = %d, want 1 (no re-fetch)", n)
	}

	// Simulate a new process: only on-disk last-seen state should remain.
	Version = "v1.0.0"
	if got := AutoUpdate(); got != AutoUpdateNoChange {
		t.Fatalf("new-process AutoUpdate() = %v, want AutoUpdateNoChange", got)
	}
	if n := f.binaryGETs.Load(); n != 1 {
		t.Fatalf("binary GETs after new-process AutoUpdate = %d, want 1", n)
	}
}

func TestParseAdvertisedVersion(t *testing.T) {
	ok := []string{"v1.1.1", "  v1.1.1\n", "v1.1.1-4-gabcdef", "abc1234-dirty-20260116120000"}
	for _, raw := range ok {
		got, err := parseAdvertisedVersion(raw)
		if err != nil {
			t.Fatalf("parseAdvertisedVersion(%q) error = %v", raw, err)
		}
		if got != strings.TrimSpace(raw) {
			t.Fatalf("parseAdvertisedVersion(%q) = %q", raw, got)
		}
	}

	bad := []string{"", "  \n", "<html>", "v1.1.1 extra", "not a version!!!"}
	for _, raw := range bad {
		if _, err := parseAdvertisedVersion(raw); err == nil {
			t.Fatalf("parseAdvertisedVersion(%q) succeeded, want error", raw)
		}
	}
}
