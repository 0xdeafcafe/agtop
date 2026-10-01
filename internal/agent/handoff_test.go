package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/0xdeafcafe/rush/internal/agent/tool"
)

// A long conversation is handed on cut down: its latest steps and turns,
// and a long message clipped.
func TestHandoffKeepsTheLatest(t *testing.T) {
	c := Conversation{From: "nosuch", Cwd: "/r", First: strings.Repeat("word ", 2000)}
	for i := range 50 {
		c.Steps = append(c.Steps, Step{Call: tool.Call{Kind: tool.Shell, Input: tool.Input{Description: fmt.Sprintf("step %d", i)}}})
	}
	for i := range 10 {
		c.Recent = append(c.Recent, Line{Role: "user", Text: fmt.Sprintf("turn %d", i)})
	}
	text := Handoff(c).Text
	for _, want := range []string{"ran in nosuch", "It worked in /r.", "(20 earlier steps)", "- step 49", "turn 9", " …"} {
		if !strings.Contains(text, want) {
			t.Errorf("lacks %q:\n%s", want, text)
		}
	}
	for _, not := range []string{"- step 19\n", "turn 3\n"} {
		if strings.Contains(text, not) {
			t.Errorf("keeps %q", not)
		}
	}
	if len(text) > 12000 {
		t.Errorf("hand-off is %d bytes", len(text))
	}
}

func TestHandoffReportsOmittedCallsWithoutSummaries(t *testing.T) {
	text := Handoff(Conversation{EarlierSteps: 12}).Text
	if !strings.Contains(text, "12 earlier tool calls omitted") {
		t.Fatal("lost omission count when recent calls have no summary")
	}
}
