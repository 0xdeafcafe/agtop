package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/agent/event"
)

// linesAgent's history is a prompt and an answer per line of its file.
type linesAgent struct{ installedAgent }

func init() { agent.Register(linesAgent{}) }

func (linesAgent) Kind() agent.Kind { return "lines" }

func (linesAgent) History(s agent.Session, _ time.Time) ([]event.Event, error) {
	b, err := os.ReadFile(s.Transcript)
	if err != nil {
		return nil, err
	}
	var out []event.Event
	for _, l := range strings.Fields(string(b)) {
		out = append(out, event.Message{Role: "assistant", Parts: []event.Part{{Kind: event.Text, Text: l}}},
			event.TurnEnd{Reason: "done"})
	}
	return out, nil
}

func TestAHistoryIsFollowedAsItGrows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(path, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &history{kind: "lines", s: agent.Session{Transcript: path}}
	h.stat()
	c := &hostConn{sess: agentHistory(h.kind, h.s, time.Time{}), hist: h}
	if n := len(c.sess.Turns); n != 1 {
		t.Fatalf("read %d turns, want 1", n)
	}
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.followHistory()
	if n := len(c.sess.Turns); n != 2 {
		t.Errorf("after it grew: %d turns, want 2", n)
	}
	was := c.sess
	c.followHistory()
	if c.sess != was {
		t.Error("read again with nothing changed")
	}
}
