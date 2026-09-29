package claude

import "testing"

// A switch keeps the MCP servers' logins the folder holds now, not the
// other account's older ones.
func TestSwitchKeepsMCPLogins(t *testing.T) {
	now := []byte(`{"claudeAiOauth":{"refreshToken":"a"},"mcpOAuth":{"posthog":{"accessToken":"p"}}}`)
	to := []byte(`{"claudeAiOauth":{"refreshToken":"b"},"mcpOAuth":{"old":{}}}`)
	if got := string(withMCPLogins(to, now)); got != `{"claudeAiOauth":{"refreshToken":"b"},"mcpOAuth":{"posthog":{"accessToken":"p"}}}` {
		t.Fatalf("merged: %s", got)
	}
	if got := string(withMCPLogins(to, []byte(`{"claudeAiOauth":{}}`))); got != string(to) {
		t.Fatalf("with no sign-in now to go by: %s", got)
	}
	if got := string(withMCPLogins(to, []byte(`{"claudeAiOauth":{"refreshToken":"a"}}`))); got != `{"claudeAiOauth":{"refreshToken":"b"}}` {
		t.Fatalf("signed out of every MCP server now: %s", got)
	}
}
