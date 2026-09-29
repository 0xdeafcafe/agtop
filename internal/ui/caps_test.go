package ui

import (
	"testing"

	"github.com/0xdeafcafe/agtop/internal/convo"
)

func TestOtherAgentsOnlyGetWhatTheyCanDo(t *testing.T) {
	claude := &hostConn{kind: "claude", sess: convo.New()}
	other := &hostConn{kind: "claude", sess: convo.New()}
	other.sess.Info.Kind = "installed" // with_test's agent: no capabilities

	for _, name := range []string{"rewind", "fork", "btw", "tasks"} {
		if !canRun(claude, name) {
			t.Errorf("Claude Code can't /%s", name)
		}
		if canRun(other, name) {
			t.Errorf("an agent without it can /%s", name)
		}
	}
	for _, name := range []string{"model", "stop", "diff", "copy"} {
		if !canRun(other, name) {
			t.Errorf("any agent should /%s", name)
		}
	}
	if len(sessionCommands(other)) >= len(sessionCommands(claude)) {
		t.Error("another agent is offered every command Claude Code is")
	}
}
