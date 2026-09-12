package proxy

import (
	"bytes"
	"strings"
	"testing"
)

func TestCLIConfigureStdinAndStatus(t *testing.T) {
	testHome(t)
	doc := "[Proxy]\nN = snell, 198.51.100.5, 440, psk=clipsk, version=4\n"
	in := strings.NewReader(doc)
	var out, errw bytes.Buffer
	if err := HandleIO([]string{"configure", "--stdin"}, in, &out, &errw); err != nil {
		t.Fatalf("configure: %v", err)
	}
	if !strings.Contains(out.String(), "configured") {
		t.Fatalf("configure output %q", out.String())
	}
	if strings.Contains(out.String(), "clipsk") || strings.Contains(out.String(), "198.51.100.5") {
		t.Fatalf("configure leaked secrets: %q", out.String())
	}

	out.Reset()
	if err := HandleIO([]string{"status"}, strings.NewReader(""), &out, &errw); err != nil {
		t.Fatalf("status: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "proxy: disabled") {
		t.Fatalf("status after configure should stay disabled: %q", s)
	}
	if !strings.Contains(s, "configured: yes") {
		t.Fatalf("status %q", s)
	}
	if strings.Contains(s, "clipsk") || strings.Contains(s, "198.51.100.5") {
		t.Fatalf("status leaked secrets: %q", s)
	}

	out.Reset()
	if err := HandleIO([]string{"enable"}, strings.NewReader(""), &out, &errw); err != nil {
		t.Fatalf("enable: %v", err)
	}
	out.Reset()
	if err := HandleIO([]string{"status"}, strings.NewReader(""), &out, &errw); err != nil {
		t.Fatalf("status2: %v", err)
	}
	if !strings.Contains(out.String(), "proxy: enabled") {
		t.Fatalf("enabled status %q", out.String())
	}
}

func TestCLIUnknown(t *testing.T) {
	testHome(t)
	var out, errw bytes.Buffer
	err := HandleIO([]string{"nope"}, strings.NewReader(""), &out, &errw)
	if err == nil {
		t.Fatal("expected usage error")
	}
}
