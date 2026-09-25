package host

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/claude"
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
	// Registered after Setenv, so it runs before AGTOP_HOME is restored:
	// the hosts the test started, and any a restart left, must not outlive
	// it, and only this test's home is looked at.
	t.Cleanup(func() {
		if os.Getenv("AGTOP_HOME") != home {
			return
		}
		for _, i := range List() {
			if i.HostPID > 0 && i.HostPID != os.Getpid() {
				_ = syscall.Kill(i.HostPID, syscall.SIGKILL)
			}
		}
	})
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

func TestQueueEdits(t *testing.T) {
	setup(t)
	s := &server{cfg: Config{ID: "q"}, clients: map[*conn]struct{}{}}
	s.info.Queue = []string{"a", "b", "c", "d"}
	steps := []struct {
		o    op
		want string
	}{
		{op{Op: "queue_edit", Index: 1, Text: "B"}, "a|B|c|d"},
		{op{Op: "queue_move", Index: 3, To: 0}, "d|a|B|c"},
		{op{Op: "queue_merge", Index: 1}, "d|a\n\nB|c"},
		{op{Op: "queue_remove", Index: 0}, "a\n\nB|c"},
		{op{Op: "queue_move", Index: 0, To: 9}, "c|a\n\nB"},
	}
	for _, st := range steps {
		if err := s.editQueue(st.o); err != nil {
			t.Fatalf("%s: %v", st.o.Op, err)
		}
		if got := strings.Join(s.info.Queue, "|"); got != st.want {
			t.Fatalf("%s: got %q want %q", st.o.Op, got, st.want)
		}
	}
	if err := s.editQueue(op{Op: "queue_merge", Index: 1}); err == nil {
		t.Error("merging the last item should fail")
	}
	if err := s.editQueue(op{Op: "queue_remove", Index: 5}); err == nil {
		t.Error("removing past the end should fail")
	}
}

// stallClaude fails its first turn with an API error or a usage limit, as
// asked, and succeeds on "continue".
const stallClaude = `#!/bin/sh
echo '{"type":"system","subtype":"init","session_id":"SID","model":"claude-haiku-4-5"}'
while read -r line; do
  case "$line" in
  *overload*)
    echo '{"type":"assistant","message":{"id":"m1","role":"assistant","usage":{"input_tokens":1,"output_tokens":1},"content":[{"type":"text","text":"trying"}]}}'
    echo '{"type":"result","subtype":"error_during_execution","is_error":true,"result":"API Error: 529 Overloaded"}' ;;
  *limit*)
    echo '{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":'$(( $(date +%s) + 3600 ))',"rateLimitType":"five_hour"}}'
    echo '{"type":"result","subtype":"error_during_execution","is_error":true,"result":"Claude usage limit reached"}' ;;
  *continue*)
    echo '{"type":"result","subtype":"success","result":"Recovered."}' ;;
  esac
done
`

func TestRetryAndLimit(t *testing.T) {
	bin := setup(t)
	if err := os.WriteFile(bin, []byte(stallClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, err := Spawn(Config{Cwd: filepath.Dir(bin), Binary: bin, Prompt: "please overload", RetryBase: Duration(300 * time.Millisecond)})
	if err != nil {
		t.Fatal(err)
	}
	c, err := Dial(cfg.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// The error schedules a retry; the retry sends "continue" and recovers.
	r := next(t, c, func(ev any) bool { i, ok := ev.(InfoEvent); return ok && i.Info.Retry != nil }).(InfoEvent).Info.Retry
	if r.Attempt != 1 || r.GaveUp || !strings.Contains(r.Reason, "529") {
		t.Fatalf("retry: %+v", r)
	}
	next(t, c, func(ev any) bool { s, ok := ev.(Sent); return ok && s.Text == "continue" })
	res := next(t, c, func(ev any) bool { _, ok := ev.(headless.Result); return ok }).(headless.Result)
	if res.Text != "Recovered." {
		t.Fatalf("after retry: %+v", res)
	}
	next(t, c, func(ev any) bool { i, ok := ev.(InfoEvent); return ok && i.Info.Retry == nil && i.Info.State == "idle" })

	// A usage limit asks whether to continue at the reset (opt-in).
	if err := c.Send("hit the limit"); err != nil {
		t.Fatal(err)
	}
	l := next(t, c, func(ev any) bool { i, ok := ev.(InfoEvent); return ok && i.Info.Limit != nil }).(InfoEvent).Info.Limit
	if !l.Ask || l.Continue || l.Window != "five_hour" || time.Until(l.ResetsAt) < 50*time.Minute {
		t.Fatalf("limit: %+v", l)
	}
	if err := c.ContinueAtReset(true); err != nil {
		t.Fatal(err)
	}
	l = next(t, c, func(ev any) bool { i, ok := ev.(InfoEvent); return ok && i.Info.Limit != nil && !i.Info.Limit.Ask }).(InfoEvent).Info.Limit
	if !l.Continue {
		t.Fatalf("after yes: %+v", l)
	}
	// While waiting for the reset, a new message queues instead of going now.
	if err := c.Send("then do this"); err != nil {
		t.Fatal(err)
	}
	q := next(t, c, func(ev any) bool { i, ok := ev.(InfoEvent); return ok && len(i.Info.Queue) > 0 }).(InfoEvent).Info.Queue
	if q[0] != "then do this" {
		t.Fatalf("queue: %v", q)
	}
	_ = c.Stop()
}

func TestRetryGivesUpPastTheCache(t *testing.T) {
	s := &server{cfg: Config{ID: "x", RetryBase: Duration(time.Minute)}, clients: map[*conn]struct{}{}}
	s.info.CacheWarm = time.Now().Add(90 * time.Second)
	s.retry("API Error: 529")
	if s.info.Retry.GaveUp || s.info.Retry.Attempt != 1 {
		t.Fatalf("first retry fits in the cache window: %+v", s.info.Retry)
	}
	s.wake.Stop()
	s.retry("API Error: 529") // the second would wait 2m, past the cache
	if !s.info.Retry.GaveUp || !strings.Contains(s.info.Retry.Why, "cache") {
		t.Fatalf("should give up past the cache: %+v", s.info.Retry)
	}
}

func TestOwnTrafficStaysInHost(t *testing.T) {
	for line, want := range map[string]bool{
		`{"type":"control_request","request_id":"1","request":{"subtype":"mcp_message","server_name":"agtop","message":{}}}`:         true,
		`{"type":"control_request","request_id":"2","request":{"subtype":"can_use_tool","tool_name":"mcp__agtop__show","input":{}}}`: true,
		`{"type":"control_request","request_id":"3","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{}}}`:             false,
		`{"type":"control_request","request_id":"4","request":{"subtype":"mcp_message","server_name":"other","message":{}}}`:         false,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"mcp__agtop__show"}]}}`:                                 false,
	} {
		if got := ownTraffic([]byte(line)); got != want {
			t.Errorf("ownTraffic(%s) = %v", line, got)
		}
	}
}

// A rewind switches the host to the cut conversation and keeps the path it
// leaves as a branch; going back down that branch keeps this one in turn.
func TestRewindKeepsBranches(t *testing.T) {
	setup(t)
	if err := os.MkdirAll(dir("r"), 0o700); err != nil {
		t.Fatal(err)
	}
	s := &server{cfg: Config{ID: "r", SessionID: "old", Resume: true}, began: true, clients: map[*conn]struct{}{}}
	s.ring, s.ringN = [][]byte{[]byte(`{"type":"assistant"}`)}, 20
	s.info.State = "working"
	if err := s.rewind("cut", true, &Branch{From: 3}); err == nil {
		t.Fatal("rewinding mid-turn should be refused")
	}
	s.info.State = "idle"
	if err := s.rewind("cut", true, &Branch{From: 3, Turns: 5, Last: "try the cache"}); err != nil {
		t.Fatal(err)
	}
	if s.cfg.SessionID != "cut" || !s.began || len(s.ring) != 0 || s.info.RewoundAt.IsZero() || s.info.SessionID != "cut" {
		t.Fatalf("after rewind: cfg %+v ring %d info %+v", s.cfg, len(s.ring), s.info)
	}
	if b := s.cfg.Branches; len(b) != 1 || b[0].SessionID != "old" || b[0].From != 3 || b[0].Last != "try the cache" {
		t.Fatalf("branches %+v", b)
	}
	saved, err := ReadConfig("r")
	if err != nil || saved.SessionID != "cut" || len(saved.Branches) != 1 {
		t.Fatalf("saved %+v %v", saved, err)
	}
	// Back down the old path: it stops being a branch, and "cut" becomes one.
	if err := s.rewind("old", true, &Branch{From: 3, Turns: 2}); err != nil {
		t.Fatal(err)
	}
	if b := s.cfg.Branches; s.cfg.SessionID != "old" || len(b) != 1 || b[0].SessionID != "cut" {
		t.Fatalf("after switching back: %s %+v", s.cfg.SessionID, b)
	}
	// Before the first message: a fresh conversation, nothing to resume.
	if err := s.rewind("fresh", false, &Branch{From: 1, Turns: 5}); err != nil {
		t.Fatal(err)
	}
	if s.began || s.cfg.Resume || len(s.cfg.Branches) != 2 {
		t.Fatalf("fresh: began %v cfg %+v", s.began, s.cfg)
	}
}

// A host from before rewind is restarted on this build, already rewound,
// with the path it left kept as a branch.
func TestRewindByRestart(t *testing.T) {
	bin := setup(t)
	acct := claude.Account{Name: "t", ConfigDir: t.TempDir()}
	cfg, err := Spawn(Config{Cwd: filepath.Dir(bin), Binary: bin, Account: acct, Resume: true, SessionID: "old-0000-aaaa"})
	if err != nil {
		t.Fatal(err)
	}
	old, _ := ReadInfo(cfg.ID)
	// The old conversation has a transcript, so it's worth keeping.
	tp := acct.TranscriptPath(cfg.Cwd, cfg.SessionID)
	os.MkdirAll(filepath.Dir(tp), 0o700)
	os.WriteFile(tp, []byte("{}\n"), 0o600)
	if err := RewindByRestart(cfg.ID, "cut-0000-bbbb", true, Branch{From: 2, Turns: 3}); err != nil {
		t.Fatal(err)
	}
	info, _ := ReadInfo(cfg.ID)
	got, _ := ReadConfig(cfg.ID)
	if info.HostPID == old.HostPID || !alive(info.HostPID) || info.Proto != Proto || info.SessionID != "cut-0000-bbbb" {
		t.Fatalf("restarted: %+v (was pid %d)", info, old.HostPID)
	}
	if got.SessionID != "cut-0000-bbbb" || len(got.Branches) != 1 || got.Branches[0].SessionID != "old-0000-aaaa" || got.Branches[0].From != 2 {
		t.Fatalf("config %+v", got)
	}
	if c, err := Dial(cfg.ID); err == nil {
		c.Stop()
		c.Close()
	}
}

func TestRingTrimsWholeTurns(t *testing.T) {
	setup(t)
	if err := os.MkdirAll(dir("tr"), 0o700); err != nil {
		t.Fatal(err)
	}
	s := &server{cfg: Config{ID: "tr"}, clients: map[*conn]struct{}{}}
	big := []byte(`{"type":"assistant","message":{"content":"` + strings.Repeat("x", 1<<20) + `"}}`)
	for turn := range 5 {
		echo, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": fmt.Sprint("turn ", turn)}, "agtop_sent": true})
		s.record(echo)
		for range 3 {
			s.record(append([]byte(nil), big...))
		}
	}
	if s.ringN > ringMax {
		t.Fatalf("ring holds %d bytes, over %d", s.ringN, ringMax)
	}
	from, ok := turnStart(s.ring[0], s.ring[1])
	if !ok {
		t.Fatalf("the ring should start at a turn, starts with %.60s", s.ring[0])
	}
	if !s.info.ReplayFrom.Equal(from) {
		t.Fatalf("ReplayFrom is %v, the ring starts at %v", s.info.ReplayFrom, from)
	}
	if !bytes.Contains(s.ring[1], []byte("turn 3")) {
		t.Fatalf("the ring should keep the last two turns, starts with %.80s", s.ring[1])
	}

	// A turn bigger than the ring keeps its latest part and still says
	// when it began.
	var last time.Time
	for i := range s.ring[:len(s.ring)-1] {
		if t, ok := turnStart(s.ring[i], s.ring[i+1]); ok {
			last = t
		}
	}
	for range 10 {
		s.record(append([]byte(nil), big...))
	}
	if s.ringN > ringMax {
		t.Fatalf("ring holds %d bytes, over %d", s.ringN, ringMax)
	}
	if !s.info.ReplayFrom.Equal(last) {
		t.Fatalf("ReplayFrom is %v, the last turn began at %v", s.info.ReplayFrom, last)
	}
}
