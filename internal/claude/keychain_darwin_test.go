package claude

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// A sign-in with MCP servers' logins in it runs past what one of
// security's prompt lines holds; it must still be saved whole.
func TestKeychainWriteLong(t *testing.T) {
	if os.Getenv("AGTOP_KEYCHAIN_TEST") == "" {
		t.Skip("writes to your login keychain; set AGTOP_KEYCHAIN_TEST=1")
	}
	const svc = "agtop-test"
	defer keychainDelete(svc, "t")
	for _, n := range []int{100, 2719, 6000} {
		secret := []byte(`{"claudeAiOauth":{"refreshToken":"` + strings.Repeat("x", n) + `"}}`)
		if err := keychainWrite(svc, "t", secret); err != nil {
			t.Fatalf("%d bytes: %v", n, err)
		}
		got, err := keychainRead(svc, "t")
		if err != nil || !bytes.Equal(got, secret) {
			t.Fatalf("%d bytes: read back %d bytes, %v", n, len(got), err)
		}
	}
}

// Claude Code reads its sign-in under your user name. Another item under
// the same service (an older tool's) must not catch what a switch writes.
func TestCredsGoToClaudeCodesItem(t *testing.T) {
	if os.Getenv("AGTOP_KEYCHAIN_TEST") == "" {
		t.Skip("writes to your login keychain; set AGTOP_KEYCHAIN_TEST=1")
	}
	a := Account{ConfigDir: t.TempDir()}
	svc := a.keychainService()
	defer keychainDelete(svc, "unknown")
	defer keychainDelete(svc, keychainUser())
	if err := keychainWrite(svc, "unknown", []byte(`{"claudeAiOauth":{"refreshToken":"stale"}}`)); err != nil {
		t.Fatal(err)
	}
	want := []byte(`{"claudeAiOauth":{"refreshToken":"new"}}`)
	if err := writeCreds(a, want); err != nil {
		t.Fatal(err)
	}
	if got, err := keychainRead(svc, keychainUser()); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("Claude Code's item: %s, %v", got, err)
	}
	if got, err := readCreds(a); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("read back: %s, %v", got, err)
	}
}
