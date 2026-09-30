package acp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xdeafcafe/rush/internal/agent"
)

func TestCheckKey(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	g, p := Known[0], agent.Profile{Dir: t.TempDir()}
	if g.CheckKey(p) == nil {
		t.Fatal("gemini with no creds and no key reads as signed in")
	}
	os.WriteFile(filepath.Join(p.Dir, "oauth_creds.json"), []byte("{}"), 0o600)
	if err := g.CheckKey(p); err != nil {
		t.Fatalf("signed in with Google: %v", err)
	}
	t.Setenv("GEMINI_API_KEY", "k")
	if err := g.CheckKey(agent.Profile{Dir: t.TempDir()}); err != nil {
		t.Fatalf("with a key: %v", err)
	}
	if err := Known[1].CheckKey(agent.Profile{Dir: t.TempDir()}); err != nil {
		t.Fatalf("kimi has no check: %v", err)
	}
}
