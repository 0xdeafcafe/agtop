package state

import (
	"testing"

	"github.com/0xdeafcafe/agtop/internal/claude"
)

// New sessions run in the home of the login in use, and in ~/.claude
// when none is, or it's one agtop no longer keeps.
func TestRunAccount(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	cfg := Config{Logins: []claude.Login{{ID: "a", Name: "work"}}}
	if !cfg.RunAccount().IsDefault() {
		t.Fatal("with no login in use, sessions run in ~/.claude")
	}
	if err := SetClaudeUsing("a"); err != nil {
		t.Fatal(err)
	}
	if got := cfg.RunAccount(); got.ConfigDir != ClaudeHome("a").ConfigDir || got.Name != "work" {
		t.Fatalf("the login in use runs in its home: %+v", got)
	}
	if err := SetClaudeUsing("gone"); err != nil {
		t.Fatal(err)
	}
	if !cfg.RunAccount().IsDefault() {
		t.Fatal("a login agtop doesn't keep runs nothing: ~/.claude")
	}
	if err := SetClaudeUsing(""); err != nil || ClaudeUsing() != "" {
		t.Fatalf("back to ~/.claude: %q, %v", ClaudeUsing(), err)
	}
}
