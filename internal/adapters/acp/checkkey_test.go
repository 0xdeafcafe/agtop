package acp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xdeafcafe/rush/internal/agent"
)

func TestCheckKey(t *testing.T) {
	t.Setenv("RUSH_TEST_KEY", "")
	g := Agent{ID: "x", Title: "X", Creds: []string{"oauth_creds.json"}, Keys: []string{"RUSH_TEST_KEY"}}
	p := agent.Profile{Dir: t.TempDir()}
	if g.CheckKey(p) == nil {
		t.Fatal("no creds and no key reads as signed in")
	}
	os.WriteFile(filepath.Join(p.Dir, "oauth_creds.json"), []byte("{}"), 0o600)
	if err := g.CheckKey(p); err != nil {
		t.Fatalf("signed in with Google: %v", err)
	}
	t.Setenv("RUSH_TEST_KEY", "k")
	if err := g.CheckKey(agent.Profile{Dir: t.TempDir()}); err != nil {
		t.Fatalf("with a key: %v", err)
	}
	if err := Known[0].CheckKey(agent.Profile{Dir: t.TempDir()}); err != nil {
		t.Fatalf("kimi has no check: %v", err)
	}
}
