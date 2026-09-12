package userctx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHomeDirHonorsTarishHomeUnderSudoEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TARISH_HOME", home)
	t.Setenv("TARISH_USER", "operator")
	t.Setenv("SUDO_USER", "operator")
	t.Setenv("HOME", "/root")

	got, err := HomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != home {
		t.Fatalf("HomeDir=%q want %q", got, home)
	}
	ident, err := Current()
	if err != nil {
		t.Fatal(err)
	}
	if ident.Username != "operator" {
		t.Fatalf("username=%q", ident.Username)
	}
	_ = filepath.Separator
	_ = os.DevNull
}
