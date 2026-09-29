package ui

import (
	"testing"

	"github.com/0xdeafcafe/rush/internal/convo"
)

func TestOtherAgentsOnlyGetWhatTheyCanDo(t *testing.T) {
	claude := &hostConn{kind: "claude", sess: convo.New()}
	other := &hostConn{kind: "claude", sess: convo.New()}
	other.sess.Info.Kind = "installed" // with_test's agent: no capabilities

	for _, name := range []string{"rewind", "fork", "btw", "tasks", "model", "compact", "context", "plugin", "hooks", "memory"} {
		if !canRun(claude, name) {
			t.Errorf("Claude Code can't /%s", name)
		}
		if canRun(other, name) {
			t.Errorf("an agent without it can /%s", name)
		}
	}
	for _, name := range []string{"stop", "diff", "copy", "status", "config"} {
		if !canRun(other, name) {
			t.Errorf("any agent should /%s", name)
		}
	}
	if len(sessionCommands(other)) >= len(sessionCommands(claude)) {
		t.Error("another agent is offered every command Claude Code is")
	}
}

// agentCanRun is the same lookup canRun does, usable with no session open:
// what the command bar checks.
func TestAgentCanRun(t *testing.T) {
	if !agentCanRun("claude", "fork") {
		t.Error("Claude Code can't fork")
	}
	if agentCanRun("installed", "fork") {
		t.Error("an agent without it can fork")
	}
	if !agentCanRun("installed", "stop") {
		t.Error("any agent should stop")
	}
}
