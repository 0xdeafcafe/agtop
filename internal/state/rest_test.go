package state

import (
	"testing"
	"time"
)

func TestRestForPrefersProfileThenAgent(t *testing.T) {
	c := Config{Profiles: []Profile{{Name: "slow", RestMinutes: 120}, {Name: "plain"}}}
	c.Dispatch.RestMinutes = 30
	c.Dispatch.Starts = map[string]Start{"ollama-codex": {RestMinutes: 240}}
	for _, tc := range []struct {
		profile, kind string
		want          time.Duration
		ok            bool
	}{
		{"slow", "ollama-codex", 120 * time.Minute, true},
		{"plain", "ollama-codex", 240 * time.Minute, true},
		{"plain", "kimi", 30 * time.Minute, false},
		{"", "", 30 * time.Minute, false},
	} {
		if got, ok := c.RestFor(tc.profile, tc.kind); got != tc.want || ok != tc.ok {
			t.Fatalf("%s/%s: %v %v, want %v %v", tc.profile, tc.kind, got, ok, tc.want, tc.ok)
		}
	}
}
