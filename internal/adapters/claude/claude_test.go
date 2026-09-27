package claude

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/agent/event"
	"github.com/0xdeafcafe/agtop/internal/agent/tool"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/state"
)

func TestRegistered(t *testing.T) {
	a, ok := agent.Get(Kind)
	if !ok || a.Name() != "Claude Code" || !a.Caps().Has(agent.CapRewind|agent.CapQuestions) {
		t.Fatalf("Get(claude) = %v, %v", a, ok)
	}
	if _, ok := a.(agent.Driver); !ok {
		t.Error("Claude Code can't be driven")
	}
}

func TestProfiles(t *testing.T) {
	a := Adapter{Config: func() state.Config {
		return state.Config{Folders: []claude.Account{{Name: "work", ConfigDir: "/x/.claude-work"}}}
	}}
	ps := a.Profiles()
	if len(ps) < 2 || ps[len(ps)-1] != (agent.Profile{Kind: Kind, Name: "work", Dir: "/x/.claude-work"}) {
		t.Errorf("Profiles = %+v", ps)
	}
}

// Lines as Claude Code writes them, trimmed.
const (
	lineInit = `{"type":"system","subtype":"init","cwd":"/work","session_id":"s-1","tools":["Bash"],"model":"claude-haiku-4-5","permissionMode":"default","claude_code_version":"2.1.280"}`
	lineUse  = `{"type":"assistant","message":{"model":"claude-haiku-4-5","id":"msg_1","role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"echo hi"}}]},"session_id":"s-1"}`
	lineAsk  = `{"type":"control_request","request_id":"req-1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"echo hi"},"tool_use_id":"toolu_1"}}`
	lineQ    = `{"type":"control_request","request_id":"req-2","request":{"subtype":"can_use_tool","tool_name":"AskUserQuestion","input":{"questions":[{"question":"Which?","header":"Pick","multiSelect":true,"options":[{"label":"A"},{"label":"B"}]}]},"tool_use_id":"toolu_2"}}`
	lineRes  = `{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_1","type":"tool_result","content":[{"type":"text","text":"hi"}],"is_error":false}]},"session_id":"s-1"}`
	lineEnd  = `{"type":"result","subtype":"success","is_error":false,"duration_ms":1200,"num_turns":1,"result":"Done.","session_id":"s-1","total_cost_usd":0.01,"usage":{"input_tokens":5,"output_tokens":7}}`
)

// fakeClaude answers like claude -p: it reads the initialize request and a
// prompt, asks to run a command and a question, and logs what it's sent.
func fakeClaude(t *testing.T) (bin, log string) {
	dir := t.TempDir()
	log = filepath.Join(dir, "in.log")
	script := `#!/bin/sh
printf '%s\n' "$*" > "` + log + `.args"
read -r init; printf '%s\n' "$init" >> "` + log + `"
read -r prompt; printf '%s\n' "$prompt" >> "` + log + `"
echo '` + lineInit + `'
echo '` + lineUse + `'
echo '` + lineAsk + `'
read -r answer; printf '%s\n' "$answer" >> "` + log + `"
echo '` + lineQ + `'
read -r answer; printf '%s\n' "$answer" >> "` + log + `"
echo '` + lineRes + `'
echo '` + lineEnd + `'
while read -r more; do printf '%s\n' "$more" >> "` + log + `"; done
`
	bin = filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, log
}

func TestDriverRoundTrip(t *testing.T) {
	bin, log := fakeClaude(t)
	conn, err := Adapter{}.Start(context.Background(), agent.StartOptions{Dir: t.TempDir(), SessionID: "s-1", Resume: true, Fork: true, Binary: bin})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.Send(agent.Input{Text: "say hi"}); err != nil {
		t.Fatal(err)
	}
	var got []event.Event
	timeout := time.After(5 * time.Second)
loop:
	for {
		select {
		case ev := <-conn.Events():
			got = append(got, ev)
			switch ev := ev.(type) {
			case event.Approval:
				if err := conn.Answer(ev.ID, "allow"); err != nil {
					t.Fatal(err)
				}
			case event.Question:
				if err := conn.(agent.Answerer).AnswerQuestion(ev.ID, map[string][]string{"Which?": {"A", "B"}}); err != nil {
					t.Fatal(err)
				}
			case event.TurnEnd:
				break loop
			}
		case <-timeout:
			t.Fatalf("timed out after %d events: %#v", len(got), got)
		}
	}
	if m, ok := got[1].(event.Message); !ok || m.Parts[0].Call.Kind != tool.Shell {
		t.Errorf("second event = %#v, want the Bash call", got[1])
	}
	if err := conn.Answer("req-1", "allow"); err == nil {
		t.Error("an approval answered twice")
	}
	conn.Close()
	in, _ := os.ReadFile(log)
	args, _ := os.ReadFile(log + ".args")
	for _, want := range []string{`"subtype":"initialize"`, `"behavior":"allow"`, `"Which?":"A, B"`} {
		if !strings.Contains(string(in), want) {
			t.Errorf("claude was sent %s, missing %s", in, want)
		}
	}
	for _, want := range []string{"--resume s-1", "--fork-session"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("args %q missing %q", args, want)
		}
	}
}

func TestAccounts(t *testing.T) {
	a := Adapter{Config: func() state.Config {
		return state.Config{Logins: []claude.Login{{Name: "work", ID: "u-1", Email: "me@x"}}}
	}}
	accts := a.Accounts()
	if len(accts) != 1 || accts[0].Key != "claude:login:u-1" || accts[0].Email != "me@x" {
		t.Fatalf("Accounts = %+v", accts)
	}
	if l, ok := a.login(accts[0]); !ok || l.ID != "u-1" {
		t.Errorf("login(%+v) = %+v, %v", accts[0], l, ok)
	}
	if err := a.Switch(agent.Profile{Dir: t.TempDir()}, agent.Account{Key: "claude:login:nobody"}); err == nil {
		t.Error("switched to a login agtop doesn't have")
	}
}
