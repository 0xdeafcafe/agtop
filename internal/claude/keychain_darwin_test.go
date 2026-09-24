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
