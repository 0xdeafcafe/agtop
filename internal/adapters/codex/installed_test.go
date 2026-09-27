package codex

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xdeafcafe/agtop/internal/agent"
)

// A ~/.codex left behind by an uninstalled codex isn't a profile: Codex
// shows only once its program is found.
func TestProfilesNeedCodexInstalled(t *testing.T) {
	home := t.TempDir()
	bin := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin)
	t.Setenv("CODEX_HOME", "")
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	defer agent.Recheck()
	agent.Recheck()
	if p := agent.Path(Kind); p != "" {
		t.Skipf("codex is installed where every machine looks: %s", p)
	}
	if ps := (Adapter{}).Profiles(); len(ps) != 0 {
		t.Fatalf("codex isn't installed, but Profiles = %+v", ps)
	}
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	agent.Recheck()
	if ps := (Adapter{}).Profiles(); len(ps) != 1 || ps[0].Dir != filepath.Join(home, ".codex") {
		t.Fatalf("codex is installed, Profiles = %+v", ps)
	}
}
