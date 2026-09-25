package convo

import (
	"strings"
	"testing"

	"github.com/0xdeafcafe/agtop/internal/headless"
	"github.com/0xdeafcafe/agtop/internal/host"
)

// A message draws as its card with no row over it, names the subagent it
// went to, and never folds into a run with the steps around it.
func TestMessageCard(t *testing.T) {
	s := New()
	s.Info.Cwd = "/work"
	msg := "Your session was interrupted.\n\nPick up where you left off."
	for i, e := range []any{
		host.Sent{Text: "go"},
		toolUse("a1", "Agent", map[string]any{"description": "Slim the menubar", "prompt": "…", "subagent_type": "general-purpose"}),
		toolResult("a1", "", false, map[string]any{"agentId": "ad312ddcf1d30b908", "status": "async_launched"}),
		toolUse("q1", "ToolSearch", map[string]any{"query": "select:SendMessage,Monitor", "max_results": 2}),
		toolResult("q1", "", false, map[string]any{"matches": []any{"SendMessage", "Monitor"}}),
		toolUse("m1", "SendMessage", map[string]any{"to": "ad312ddcf1d30b908", "summary": "Resume the menubar work", "message": msg}),
		toolResult("m1", "", false, map[string]any{"success": true, "message": "Resuming agent ad312dd", "resumedAgentId": "ad312ddcf1d30b908"}),
		toolUse("m2", "SendMessage", map[string]any{"to": "a0123456789abcdef", "message": "Stop there."}),
		toolResult("m2", "", false, map[string]any{"success": true, "message": "Message queued for delivery to a0123456789abcdef at its next tool round."}),
		toolUse("w1", "ScheduleWakeup", map[string]any{"delaySeconds": 1500.0, "reason": "waiting on CI", "prompt": "check"}),
		toolResult("w1", "", false, map[string]any{"scheduledFor": 0.0}),
		say("Sent."),
		headless.Result{Subtype: "success"},
	} {
		s.Apply(e, at(i))
	}
	got := plain(s.Render(Options{Width: 100, Now: at(20)}))
	for _, w := range []string{
		"✓ ⌕ loaded SendMessage, Monitor",
		"╭─ → to Slim the menubar ─",
		"│ Resume the menubar work",
		"│ Your session was interrupted.",
		"woke it up ─╯",
		"╭─ → to agent a012345 ─",
		"│ Stop there.",
		"read at its next step ─╯",
		"◔ wake in 25m",
	} {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in\n%s", w, got)
		}
	}
	for _, w := range []string{"SendMessage  ", "steps:", "• SendMessage"} {
		if strings.Contains(got, w) {
			t.Errorf("unwanted %q in\n%s", w, got)
		}
	}
	// Opened, it shows the whole message, its paragraphs apart.
	var ref string
	for _, l := range s.Render(Options{Width: 100, Now: at(20)}) {
		if strings.Contains(stripANSI(l.Text), "to Slim the menubar") {
			ref = l.Ref
		}
	}
	if !strings.HasSuffix(ref, ":s:m1") {
		t.Fatalf("the card's top edge isn't the step's row: %q", ref)
	}
	got = plain(s.Render(Options{Width: 100, Now: at(20), Open: map[string]bool{ref: true}}))
	if !strings.Contains(got, "│ Your session was interrupted.") || !strings.Contains(got, "│ Pick up where you left off.") {
		t.Errorf("opened message:\n%s", got)
	}
}
