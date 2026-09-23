package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/headless"
)

// The test binary stands in for agtop: Spawn runs `<exe> host run <id>`.
func TestMain(m *testing.M) {
	if len(os.Args) == 4 && os.Args[1] == "host" && os.Args[2] == "run" {
		if err := Run(os.Args[3]); err != nil {
			os.Stderr.WriteString(err.Error() + "\n")
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeClaude plays a turn per message: a streamed word, a Bash call that
// needs permission, its result, then the end of the turn. Each launch's
// arguments are appended to args.log.
const fakeClaude = `#!/bin/sh
printf '%s\n' "$*" >> "$(dirname "$0")/args.log"
echo '{"type":"system","subtype":"init","session_id":"SID","model":"claude-haiku-4-5","permissionMode":"default","tools":["Bash"]}'
while read -r line; do
  case "$line" in
  *'"type":"user"'*)
    echo '{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"On it"}}}'
    echo '{"type":"assistant","message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"On it"},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"echo hi"}}]}}'
    echo '{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"echo hi"},"tool_use_id":"t1"}}'
    ;;
  *'"type":"control_response"'*)
    echo '{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"hi"}]}}'
    echo '{"type":"result","subtype":"success","result":"Said hi.","total_cost_usd":0.01}'
    ;;
  esac
done
`

func setup(t *testing.T) (bin string) {
	t.Helper()
	// Short, because unix socket paths are capped near 104 bytes on macOS.
	home, err := os.MkdirTemp("/tmp", "agtop-host-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	t.Setenv("AGTOP_HOME", home)
	bin = filepath.Join(home, "claude")
	if err := os.WriteFile(bin, []byte(fakeClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// next reads host lines until one decodes to something want accepts.
func next(t *testing.T, c *Client, want func(any) bool) any {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case line, ok := <-c.Lines:
			if !ok {
				t.Fatal("connection closed")
			}
			ev, err := Decode(line)
			if err != nil {
				t.Fatalf("decode %s: %v", line, err)
			}
			if want(ev) {
				return ev
			}
		case <-timeout:
			t.Fatal("timed out")
		}
	}
}

func inState(s string) func(any) bool {
	return func(ev any) bool {
		i, ok := ev.(InfoEvent)
		return ok && i.Info.State == s
	}
}

func TestHostLifecycle(t *testing.T) {
	bin := setup(t)
	cfg, err := Spawn(Config{Cwd: filepath.Dir(bin), Prompt: "say hi", Binary: bin, IdleStop: Duration(300 * time.Millisecond)})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ID) != 8 || !strings.HasPrefix(strings.ReplaceAll(cfg.SessionID, "-", ""), cfg.ID) {
		t.Fatalf("ids: %q %q", cfg.ID, cfg.SessionID)
	}
	c, err := Dial(cfg.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ask := next(t, c, func(ev any) bool { _, ok := ev.(headless.PermissionRequest); return ok }).(headless.PermissionRequest)
	if info := next(t, c, inState("blocked")).(InfoEvent).Info; info.Needs != "Bash echo hi" {
		t.Errorf("while asking: %+v", info)
	}
	if err := c.Allow(ask.ID, nil, false); err != nil {
		t.Fatal(err)
	}
	next(t, c, func(ev any) bool { a, ok := ev.(Answered); return ok && a.ID == "r1" })
	res := next(t, c, func(ev any) bool { _, ok := ev.(headless.Result); return ok }).(headless.Result)
	if res.Text != "Said hi." {
		t.Errorf("result: %+v", res)
	}
	idle := next(t, c, inState("idle")).(InfoEvent).Info
	if idle.Detail != "Said hi." || idle.CostUSD != 0.01 || idle.ClaudePID == 0 {
		t.Errorf("idle: %+v", idle)
	}

	// Idle past IdleStop: Claude Code goes, the host stays.
	next(t, c, func(ev any) bool { i, ok := ev.(InfoEvent); return ok && i.Info.ClaudePID == 0 })

	// A second client is replayed the conversation without stream deltas.
	c2, err := Dial(cfg.ID)
	if err != nil {
		t.Fatal(err)
	}
	var replay []string
	for line := range c2.Lines {
		replay = append(replay, string(line))
		if strings.Contains(string(line), `"type":"agtop_info"`) {
			break
		}
	}
	c2.Close()
	joined := strings.Join(replay, "\n")
	if strings.Contains(joined, "stream_event") || !strings.Contains(joined, `"agtop_sent":true`) || !strings.Contains(joined, "Said hi.") {
		t.Errorf("replay:\n%s", joined)
	}

	// The next message resumes the same conversation.
	if err := c.Send("again"); err != nil {
		t.Fatal(err)
	}
	next(t, c, func(ev any) bool { _, ok := ev.(headless.PermissionRequest); return ok })
	args, _ := os.ReadFile(filepath.Join(filepath.Dir(bin), "args.log"))
	launches := strings.Split(strings.TrimSpace(string(args)), "\n")
	if len(launches) != 2 || !strings.Contains(launches[0], "--session-id "+cfg.SessionID) || !strings.Contains(launches[1], "--resume "+cfg.SessionID) {
		t.Errorf("launches:\n%s", args)
	}

	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		info, _ := ReadInfo(cfg.ID)
		if info.State == "stopped" && !alive(info.HostPID) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("host still up: %+v", info)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if list := List(); len(list) != 1 || list[0].ID != cfg.ID {
		t.Errorf("list: %+v", list)
	}
}

// TestRealHost runs two turns through a host and the installed claude, with
// Claude Code stopped for idling in between, so the second turn resumes. It
// spends a few cents of Haiku, so it only runs with AGTOP_REAL_CLAUDE=1.
func TestRealHost(t *testing.T) {
	if os.Getenv("AGTOP_REAL_CLAUDE") == "" {
		t.Skip("set AGTOP_REAL_CLAUDE=1 to run against the installed claude")
	}
	bin := setup(t)
	cfg, err := Spawn(Config{Cwd: filepath.Dir(bin), Model: "haiku", Prompt: "The secret word is PELICAN. Do not use any tools. Reply with only: ok",
		IdleStop: Duration(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	c, err := Dial(cfg.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	defer c.Stop()
	wait := func() headless.Result {
		t.Helper()
		timeout := time.After(90 * time.Second)
		for {
			select {
			case line, ok := <-c.Lines:
				if !ok {
					t.Fatal("connection closed")
				}
				switch ev, _ := Decode(line); ev := ev.(type) {
				case headless.Result:
					return ev
				case headless.PermissionRequest:
					// No tools needed; refuse anything it tries.
					_ = c.Deny(ev.ID, "No tools in this test; just answer.", false)
				}
			case <-timeout:
				log, _ := os.ReadFile(filepath.Join(dir(cfg.ID), "host.log"))
				t.Fatalf("timed out; host.log:\n%s", log)
			}
		}
	}
	if r := wait(); r.IsError {
		t.Fatalf("first turn: %+v", r)
	}
	next(t, c, func(ev any) bool { i, ok := ev.(InfoEvent); return ok && i.Info.ClaudePID == 0 })
	if err := c.Send("What is the secret word? Reply with only the word."); err != nil {
		t.Fatal(err)
	}
	r := wait()
	if !strings.Contains(strings.ToUpper(r.Text), "PELICAN") {
		t.Fatalf("resumed turn forgot: %+v", r)
	}
	info, _ := ReadInfo(cfg.ID)
	t.Logf("session %s (config %s), cost $%.4f", info.SessionID, cfg.SessionID, info.CostUSD)
	if info.SessionID != cfg.SessionID {
		t.Errorf("resume changed the session id: %s -> %s", cfg.SessionID, info.SessionID)
	}
}
