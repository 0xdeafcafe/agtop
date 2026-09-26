package claude

import "testing"

func TestKeepAsTokenOwner(t *testing.T) {
	if got := keepAs("profile", "owner", true); got != "owner" {
		t.Fatalf("known owner: %q", got)
	}
	if got := keepAs("profile", "", false); got != "profile" {
		t.Fatalf("unknown owner: %q", got)
	}
}

func TestKeychainUserIsYours(t *testing.T) {
	t.Setenv("USER", "someone")
	if got := keychainUser(); got != "someone" {
		t.Fatalf("keychainUser: %q", got)
	}
}
