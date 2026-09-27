package codex

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"

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
