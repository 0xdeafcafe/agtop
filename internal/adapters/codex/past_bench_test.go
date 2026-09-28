package codex

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent"
)

// BenchmarkPastReal lists this machine's Codex threads, as the list does
// every minute.
func BenchmarkPastReal(b *testing.B) {
	u, err := user.Current()
	if err != nil {
		b.Skip(err)
	}
	dir := filepath.Join(u.HomeDir, ".codex")
	if _, err := os.Stat(filepath.Join(dir, "sessions")); err != nil {
		b.Skip("no ~/.codex/sessions")
	}
	p := agent.Profile{Kind: Kind, Dir: dir}
	Adapter{}.Past(p)
	b.ReportAllocs()
	for b.Loop() {
		Adapter{}.Past(p)
	}
}

// BenchmarkReadHeadReal reads every rollout's head afresh, as agtop does
// for each as it starts.
func BenchmarkReadHeadReal(b *testing.B) {
	u, err := user.Current()
	if err != nil {
		b.Skip(err)
	}
	var paths []string
	filepath.WalkDir(filepath.Join(u.HomeDir, ".codex", "sessions"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Ext(p) == ".jsonl" {
			paths = append(paths, p)
		}
		return nil
	})
	if len(paths) == 0 {
		b.Skip("no rollouts")
	}
	b.ReportAllocs()
	for b.Loop() {
		for _, p := range paths {
			readHead(p, time.Time{})
		}
	}
}
