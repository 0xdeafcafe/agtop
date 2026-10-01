package ui

import (
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/agent/tool"
	"github.com/0xdeafcafe/rush/internal/convo"
	"github.com/0xdeafcafe/rush/internal/host"
)

// A subagent's Bash runs in the session's own process: the one opened is
// shown which command of its chain runs now, as the main session is.
func TestShellsReachTheOpenSubagent(t *testing.T) {
	cmd := "go build ./... && go test ./..."
	sub := convo.SubagentTail("")
	sub.Sess.Apply(host.Sent{Text: "go"}, time.Now())
	sub.Sess.Apply(event.Message{Role: "assistant", ID: "m1", Parts: []event.Part{
		{Kind: event.ToolCall, Call: &tool.Call{ID: "b1", Name: "Bash", Kind: tool.Shell, Input: tool.Input{Command: cmd}}},
	}}, time.Now())
	m := &Model{host: &hostConn{key: "k", sess: convo.New(), subTail: sub}}
	m.onShells(shellsMsg{key: "k", shells: []convo.Shell{{
		Cmd: "/bin/zsh -c eval '" + cmd + "' < /dev/null", Start: time.Now(),
		Kids: []convo.ShellProc{{Args: []string{"go", "test", "./..."}, Start: time.Now()}},
	}}})
	if rp, ok := sub.Sess.RunningPart("b1"); !ok || rp.Command == "" {
		t.Fatalf("the subagent's chain should know what runs now: %+v %v", rp, ok)
	}
}

func TestShellCommandArgv(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"/bin/zsh", "-lc", "go build && go test"}, "go build && go test"},
		{[]string{"bash", "--noprofile", "-c", "echo 'hello world'"}, "echo 'hello world'"},
		{[]string{"sh", "script.sh", "-c", "unrelated"}, ""},
		{[]string{"node", "-c", "unrelated"}, ""},
		{[]string{"bash", "--rcfile", "profile", "-c", "unrelated"}, ""},
		{[]string{"bash", "--", "-c", "unrelated"}, ""},
	} {
		if got := shellCommand(tc.args); got != tc.want {
			t.Errorf("%q: got %q want %q", tc.args, got, tc.want)
		}
	}
}
