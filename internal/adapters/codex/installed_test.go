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

// GPTs read images, Spark doesn't, and a model of no family known isn't
// known.
func TestReads(t *testing.T) {
	for model, want := range map[string]agent.Media{"gpt-5.5-codex": agent.MediaImage, "gpt-5.3-codex-spark": 0} {
		if got, ok := (Adapter{}).Reads(model); !ok || got != want {
			t.Errorf("Reads(%s) = %v, %v; want %v", model, got, ok, want)
		}
	}
	if _, ok := (Adapter{}).Reads("o9"); ok {
		t.Error("o9 is known")
	}
}
