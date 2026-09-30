package convo

import (
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/host"
)

// A turn that has heard nothing from its agent for a while is drawn as
// stalled; one the agent is still streaming into isn't.
func TestStalledTurn(t *testing.T) {
	now := time.Now()
	s := New()
	s.Apply(host.Sent{Text: "go"}, now.Add(-31*time.Minute))
	s.Apply(event.Delta{Kind: event.Thinking, Text: "hmm"}, now.Add(-31*time.Minute))
	draw := func() string { return plain(s.Render(Options{Width: 100, Now: now})) }
	if out := draw(); !strings.Contains(out, "stalled · nothing from the model for 31m") {
		t.Errorf("a turn quiet 31m isn't stalled:\n%s", out)
	}
	s.Apply(event.Delta{Kind: event.Thinking, Text: "more"}, now.Add(-10*time.Second))
	if out := draw(); strings.Contains(out, "stalled") {
		t.Errorf("a turn heard from 10s ago is stalled:\n%s", out)
	}
}
