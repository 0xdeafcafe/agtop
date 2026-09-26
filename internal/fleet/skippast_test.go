package fleet

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// A snapshot for one session leaves past conversations out: finding them
// reads every transcript and each one's repository.
func TestSkipPastLeavesPastConversationsOut(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AGTOP_HOME", filepath.Join(home, ".config", "agtop"))
	cfg := filepath.Join(home, ".claude")
	proj := filepath.Join(cfg, "projects", "-repo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"user","sessionId":"0a1b2c3d-1111-2222-3333-444455556666","cwd":"/repo","timestamp":"2026-09-26T10:00:00.000Z","message":{"role":"user","content":"hi"}}` + "\n"
	if err := os.WriteFile(filepath.Join(proj, "0a1b2c3d-1111-2222-3333-444455556666.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	store := &state.Store{}
	store.Config.Accounts = []claude.Account{{Name: "test", ConfigDir: cfg}}
	if got := store.Config.ActiveAccount().ConfigDir; got != cfg {
		t.Fatalf("account folder %s, want %s", got, cfg)
	}

	past := func(skip bool) int {
		l := NewLoader(store)
		l.SkipPast = skip
		n := 0
		for _, a := range l.Load(false).Agents {
			if a.Past {
				n++
			}
		}
		return n
	}
	if n := past(false); n != 1 {
		t.Fatalf("past conversations listed: %d, want 1", n)
	}
	if n := past(true); n != 0 {
		t.Fatalf("past conversations with SkipPast: %d, want 0", n)
	}
}
