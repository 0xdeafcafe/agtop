package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/0xdeafcafe/rush/internal/adapters/codex"
	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/convo"
	"github.com/0xdeafcafe/rush/internal/host"
)

// A live non-Claude fleet row may contain only the account name. After
// its host restarts, pre-replay history must use the saved account folder.
func TestHostedHistoryUsesSavedProfile(t *testing.T) {
	profile := t.TempDir()
	dir := filepath.Join(profile, "sessions", "2026", "10", "01")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	transcript := `{"timestamp":"2026-10-01T00:00:00Z","type":"session_meta","payload":{"id":"thread-1","cwd":"/work"}}
{"timestamp":"2026-10-01T00:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"earlier question"}]}}
{"timestamp":"2026-10-01T00:00:02Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"earlier answer"}]}}
{"timestamp":"2026-10-01T00:00:03Z","type":"event_msg","payload":{"type":"task_complete"}}
{"timestamp":"2026-10-01T00:00:05Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"live replay owns this question"}]}}
`
	if err := os.WriteFile(filepath.Join(dir, "rollout-test-thread-1.jsonl"), []byte(transcript), 0600); err != nil {
		t.Fatal(err)
	}
	info := host.Info{Kind: "codex", SessionID: "thread-1", Cwd: "/work", StartedAt: time.Date(2026, 10, 1, 0, 0, 4, 0, time.UTC)}
	for _, known := range []bool{false, true} {
		name := "missing fleet profile"
		acct := agent.Profile{Kind: "codex", Name: "account"}
		cfg := host.Config{Resume: true, Account: agent.Profile{Dir: profile}}
		if known {
			name = "existing fleet profile"
			acct.Dir = profile
			cfg.Account.Dir = t.TempDir()
		}
		t.Run(name, func(t *testing.T) {
			pre, _ := preFor(info, nil, cfg, nil, "", acct)
			if pre.other == nil || pre.otherWhole == nil {
				t.Fatal("missing history readers")
			}
			for name, read := range map[string]func() *convo.Session{"tail": pre.other, "whole": pre.otherWhole} {
				s := read()
				if len(s.Turns) != 1 || s.Turns[0].Prompt != "earlier question" {
					t.Fatalf("%s: prior history missing or replay duplicated: %d turns", name, len(s.Turns))
				}
			}
		})
	}
}
