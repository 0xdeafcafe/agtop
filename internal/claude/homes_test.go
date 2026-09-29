package claude

import (
	"testing"

	"github.com/0xdeafcafe/rush/internal/state"
)

// New sessions run in the home of the login in use, and in ~/.claude
// when none is, or it's one rush no longer keeps.
func TestRunAccount(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	cfg := state.Config{Logins: []Login{{ID: "a", Name: "work"}}}
	if !RunAccount(cfg).IsDefault() {
		t.Fatal("with no login in use, sessions run in ~/.claude")
	}
	if err := SetUsing("a"); err != nil {
		t.Fatal(err)
	}
	if got := RunAccount(cfg); got.ConfigDir != HomeOf("a").ConfigDir || got.Name != "work" {
		t.Fatalf("the login in use runs in its home: %+v", got)
	}
	if err := SetUsing("gone"); err != nil {
		t.Fatal(err)
	}
	if !RunAccount(cfg).IsDefault() {
		t.Fatal("a login rush doesn't keep runs nothing: ~/.claude")
	}
	if err := SetUsing(""); err != nil || Using() != "" {
		t.Fatalf("back to ~/.claude: %q, %v", Using(), err)
	}
}
