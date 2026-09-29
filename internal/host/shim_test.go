package host

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A stand-in runs rush spawn, and the real program when rush has gone:
// it never breaks the command.
func TestShimScript(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("RUSH_CACHE", cache)
	shims, bin := ShimDir(), filepath.Join(cache, "bin")
	for _, d := range []string{shims, bin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, body string) {
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(bin, "codex"), `echo "real $*"`+"\n")
	rush := filepath.Join(cache, "rush")
	write(rush, `echo "rush $*"`+"\n")
	run := func() string {
		cmd := exec.Command(filepath.Join(shims, "codex"), "exec", "it's")
		cmd.Env = append(os.Environ(), "PATH="+shims+string(filepath.ListSeparator)+bin+":/usr/bin:/bin")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		return string(out)
	}

	write(filepath.Join(shims, "codex"), string(shimScript(rush, "codex"))[len("#!/bin/sh\n"):])
	if got := run(); got != "rush spawn codex exec it's\n" {
		t.Errorf("with rush: %q", got)
	}
	write(filepath.Join(shims, "codex"), string(shimScript(filepath.Join(cache, "gone"), "codex"))[len("#!/bin/sh\n"):])
	if got := run(); got != "real exec it's\n" {
		t.Errorf("with rush gone: %q", got)
	}
}
