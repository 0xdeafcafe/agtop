package keychain

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// A sign-in with MCP servers' logins in it runs past what one of
// security's prompt lines holds; it must still be saved whole.
func TestWriteLong(t *testing.T) {
	if os.Getenv("RUSH_KEYCHAIN_TEST") == "" {
		t.Skip("writes to your login keychain; set RUSH_KEYCHAIN_TEST=1")
	}
	const svc = "rush-test"
	defer Delete(svc, "t")
	for _, n := range []int{100, 2719, 6000} {
		secret := []byte(`{"claudeAiOauth":{"refreshToken":"` + strings.Repeat("x", n) + `"}}`)
		if err := Write(svc, "t", secret); err != nil {
			t.Fatalf("%d bytes: %v", n, err)
		}
		got, err := Read(svc, "t")
		if err != nil || !bytes.Equal(got, secret) {
			t.Fatalf("%d bytes: read back %d bytes, %v", n, len(got), err)
		}
	}
}

// A secret of more than one line (Codex's pretty-printed auth.json) comes
// back as it went in, not as the hex security prints it as; one line of
// text that looks like hex stays text.
func TestReadsBackLines(t *testing.T) {
	if os.Getenv("RUSH_KEYCHAIN_TEST") == "" {
		t.Skip("writes to your login keychain; set RUSH_KEYCHAIN_TEST=1")
	}
	const svc = "rush-test"
	defer Delete(svc, "t")
	for _, secret := range []string{"{\n  \"a\": 1\n}", "deadbeef", `{"one":"line"}`} {
		if err := Write(svc, "t", []byte(secret)); err != nil {
			t.Fatal(err)
		}
		if got, err := Read(svc, "t"); err != nil || string(got) != secret {
			t.Errorf("wrote %q, read %q (%v)", secret, got, err)
		}
	}
}
