package vibe

import (
	"github.com/0xdeafcafe/rush/internal/agent"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestNativeLoginUsesSetupAndProfile(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "vibe")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	agent.Recheck()
	t.Cleanup(agent.Recheck)
	profile := filepath.Join(dir, "profile")
	cmd, err := (Adapter{}).SignInCommand(agent.Profile{Dir: profile})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Path != bin || !slices.Equal(cmd.Args, []string{bin, "--setup"}) || !slices.Contains(cmd.Env, "VIBE_HOME="+profile) {
		t.Fatalf("native setup command: path=%s args=%v", cmd.Path, cmd.Args)
	}
}
