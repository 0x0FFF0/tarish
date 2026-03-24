package deps

import (
	"strings"
	"testing"
)

func TestShouldEnsure(t *testing.T) {
	tests := []struct {
		name    string
		command string
		args    []string
		want    bool
	}{
		{name: "install", command: "install", want: true},
		{name: "install alias", command: "i", want: true},
		{name: "start", command: "start", want: true},
		{name: "start alias", command: "st", want: true},
		{name: "service enable", command: "service", args: []string{"enable"}, want: true},
		{name: "service enable mixed case", command: "service", args: []string{"Enable"}, want: true},
		{name: "service status", command: "service", args: []string{"status"}, want: false},
		{name: "help", command: "help", want: false},
		{name: "stop", command: "stop", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldEnsure(tt.command, tt.args); got != tt.want {
				t.Fatalf("ShouldEnsure(%q, %v) = %v, want %v", tt.command, tt.args, got, tt.want)
			}
		})
	}
}

func TestMSRServiceContent(t *testing.T) {
	wantFragments := []string{
		"Description=Apply XMRig MSR Mods for RandomX",
		"ExecStart=/usr/local/bin/randomx_boost.sh",
		"RemainAfterExit=yes",
		"WantedBy=multi-user.target",
	}

	for _, fragment := range wantFragments {
		if !strings.Contains(msrServiceContent, fragment) {
			t.Fatalf("msrServiceContent missing %q", fragment)
		}
	}
}

func TestSameFileContentIgnoresTrailingWhitespace(t *testing.T) {
	if !sameFileContent([]byte(msrServiceContent+"\n"), []byte(msrServiceContent)) {
		t.Fatal("sameFileContent should ignore trailing whitespace")
	}
}
