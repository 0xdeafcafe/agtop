package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/agent/tool"
	tokens "github.com/0xdeafcafe/rush/internal/agent/usage"
	"github.com/0xdeafcafe/rush/internal/host"
)

// A run rush can print as its program would is hosted; anything else
// runs the real program.
func TestParseRun(t *testing.T) {
	t.Setenv("VIBE_ACTIVE_MODEL", "")
	hosted := []struct {
		prog string
		args []string
		want workRun
	}{
		{"codex", []string{"exec", "say hi"}, workRun{kind: "codex", prompt: "say hi", format: "text", stdinNote: "Reading additional input from stdin..."}},
		{"codex", []string{"exec"}, workRun{kind: "codex", format: "text", stdinNote: "Reading prompt from stdin..."}}, // prompt on stdin
		{"codex", []string{"exec", "-"}, workRun{kind: "codex", format: "text"}},                                       // and so
		{"codex", []string{"exec", "--", "-v is a flag"}, workRun{kind: "codex", prompt: "-v is a flag", format: "text", stdinNote: "Reading additional input from stdin..."}},
		{"codex", []string{"-m", "gpt-5", "exec", "--json", "-o", "/tmp/o", "--skip-git-repo-check", "-C", "sub", "fix it"},
			workRun{kind: "codex", prompt: "fix it", model: "gpt-5", json: true, lastMsg: "/tmp/o", skipGit: true, cwd: "sub", format: "text",
				stdinNote: "Reading additional input from stdin..."}},
		{"codex", []string{"exec", "--full-auto", "-c", "model_reasoning_effort=high", "go"},
			workRun{kind: "codex", prompt: "go", mode: "auto", effort: "high", format: "text", stdinNote: "Reading additional input from stdin..."}},
		{"codex", []string{"e", "--sandbox=danger-full-access", "go"},
			workRun{kind: "codex", prompt: "go", mode: "full-access", format: "text", stdinNote: "Reading additional input from stdin..."}},
		{"claude", []string{"-p", "say hi"}, workRun{kind: "claude", prompt: "say hi", format: "text"}},
		{"claude", []string{"-p"}, workRun{kind: "claude", format: "text"}}, // prompt on stdin
		{"claude", []string{"-p", "--model", "haiku", "--", "--help me"}, workRun{kind: "claude", prompt: "--help me", model: "haiku", format: "text"}},
		{"claude", []string{"say hi", "--print", "--model", "haiku", "--output-format", "json"},
			workRun{kind: "claude", prompt: "say hi", model: "haiku", format: "json"}},
		{"claude", []string{"-p", "--output-format=stream-json", "--verbose", "--include-partial-messages", "--dangerously-skip-permissions", "hi"},
			workRun{kind: "claude", prompt: "hi", format: "stream-json", partial: true, mode: "bypassPermissions"}},
		{"vibe", []string{"-p", "say hi"}, workRun{kind: "vibe", prompt: "say hi", format: "text"}},
		{"vibe", []string{"--trust", "--auto-approve", "--workdir", "sub", "--prompt=go", "--output", "text"},
			workRun{kind: "vibe", prompt: "go", mode: "auto-approve", cwd: "sub", format: "text"}},
		{"kimi", []string{"-p", "go", "-m", "k2"}, workRun{kind: "kimi", prompt: "go", model: "k2", format: "text"}},
		{"opencode", []string{"run", "-m", "a/b", "fix", "it"}, workRun{kind: "opencode", prompt: "fix it", model: "a/b", format: "text"}},
	}
	for _, c := range hosted {
		got, ok := parseRun(c.prog, c.args)
		if !ok || got != c.want {
			t.Errorf("%s %q = %+v %v, want %+v", c.prog, c.args, got, ok, c.want)
		}
	}
	real := []struct {
		prog string
		args []string
	}{
		{"codex", nil},                   // its own screen
		{"codex", []string{"say hi"}},    // interactive, with a prompt
		{"codex", []string{"login"}},     // not a run
		{"codex", []string{"--version"}}, // nor this
		{"codex", []string{"exec", "resume", "--last"}},
		{"codex", []string{"exec", "--output-schema", "s.json", "go"}}, // a flag rush can't print
		{"codex", []string{"exec", "-c", "sandbox_mode=x", "go"}},
		{"codex", []string{"exec", "-s", "nope", "go"}},
		{"codex", []string{"exec", "one", "two"}},
		{"codex", []string{"exec", "--", "one", "two"}},
		{"claude", nil},
		{"claude", []string{"say hi"}}, // interactive
		{"claude", []string{"mcp", "list"}},
		{"claude", []string{"-p", "--output-format", "stream-json", "hi"}},       // claude says it wants --verbose
		{"claude", []string{"-p", "--output-format", "json", "--verbose", "hi"}}, // every message, as an array
		{"claude", []string{"-p", "--resume", "abc", "hi"}},                      // a flag rush doesn't take
		{"claude", []string{"-p", "--input-format", "stream-json"}},
		{"copilot", []string{"-p", "hi"}}, // not printed as copilot would, yet
		{"vibe", []string{"say hi"}},      // interactive, with a prompt
		{"vibe", []string{"--setup"}},
		{"vibe", []string{"-p", "hi", "--output", "json"}},                  // every message, as JSON
		{"vibe", []string{"-p", "hi", "--agent", "plan", "--auto-approve"}}, // two modes at once
		{"kimi", []string{"-p", "hi", "--yolo"}},                            // a mode rush doesn't know
		{"opencode", []string{"run"}},
		{"ls", []string{"-la"}},
	}
	for _, c := range real {
		if got, ok := parseRun(c.prog, c.args); ok {
			t.Errorf("%s %q hosted as %+v", c.prog, c.args, got)
		}
	}
}

// The real program is the first on PATH past the stand-ins, and not one
// that leads back to them.
func TestRealProgram(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("RUSH_CACHE", cache)
	shims, other, linked := host.ShimDir(), filepath.Join(cache, "bin"), filepath.Join(cache, "linked")
	for _, d := range []string{shims, other, linked} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{filepath.Join(shims, "codex"), filepath.Join(other, "codex")} {
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(shims, "codex"), filepath.Join(linked, "codex")); err != nil {
		t.Fatal(err)
	}
	path := strings.Join([]string{shims, linked, other}, string(filepath.ListSeparator))
	if got := realProgram("codex", path); got != filepath.Join(other, "codex") {
		t.Errorf("real codex %q", got)
	}
	if got := host.WithoutShims(path); got != linked+string(filepath.ListSeparator)+other {
		t.Errorf("PATH without the stand-ins %q", got)
	}
}

// codex exec's output: the last message on stdout and progress on
// stderr, or its events as JSON lines, and -o.
func TestCodexOut(t *testing.T) {
	run := []any{
		event.Init{SessionID: "th1", Model: "gpt-5", Cwd: "/w", Version: "0.155.1"},
		event.Status{Busy: true},
		event.Message{Role: "assistant", ID: "m1", Parts: []event.Part{{Kind: event.Text, Text: "I'll look."}}},
		event.Message{Role: "assistant", ID: "c1", Parts: []event.Part{{Kind: event.ToolCall, Call: &tool.Call{ID: "c1",
			Input: tool.Input{Command: "ls"},
			Raw:   []byte(`{"type":"commandExecution","id":"c1","command":"/bin/zsh -lc ls","cwd":"/w","aggregatedOutput":null,"exitCode":null,"status":"inProgress"}`)}}}},
		event.Message{Role: "user", ID: "c1:result", Parts: []event.Part{{Kind: event.ToolResult, Output: &tool.Output{CallID: "c1", Text: "a\n", Exit: new(0),
			Raw: []byte(`{"type":"commandExecution","id":"c1","command":"/bin/zsh -lc ls","cwd":"/w","aggregatedOutput":"a\n","exitCode":0,"status":"completed"}`)}}}},
		event.Message{Role: "assistant", ID: "m2", Parts: []event.Part{{Kind: event.Text, Text: "done"}}},
		event.TurnEnd{Reason: "done", Tokens: tokens.TokenUsage{Input: 100, CacheRead: 900, Output: 20}},
	}
	feed := func(r workRun) (string, string, int) {
		var out, errb bytes.Buffer
		o := &codexOut{r: r, cwd: "/w", stdout: &out, stderr: &errb}
		for _, ev := range run {
			o.line(nil, ev)
		}
		code := o.finish()
		return out.String(), errb.String(), code
	}

	last := filepath.Join(t.TempDir(), "last")
	out, errs, code := feed(workRun{kind: "codex", prompt: "look", lastMsg: last})
	if out != "done\n" || code != 0 {
		t.Errorf("stdout %q, exit %d", out, code)
	}
	for _, want := range []string{"OpenAI Codex v0.155.1\n", "workdir: /w\n", "model: gpt-5\n", "session id: th1\n", "user\nlook\n",
		"codex\nI'll look.\n", "exec\nls in /w\n", " succeeded:\na\n", "codex\ndone\n", "tokens used\n120\n"} {
		if !strings.Contains(errs, want) {
			t.Errorf("stderr lacks %q:\n%s", want, errs)
		}
	}
	if b, _ := os.ReadFile(last); string(b) != "done" {
		t.Errorf("-o wrote %q", b)
	}

	out, errs, _ = feed(workRun{kind: "codex", prompt: "look", json: true})
	want := `{"type":"thread.started","thread_id":"th1"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"I'll look."}}
{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"/bin/zsh -lc ls","aggregated_output":"","exit_code":null,"status":"in_progress"}}
{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"/bin/zsh -lc ls","aggregated_output":"a\n","exit_code":0,"status":"completed"}}
{"type":"item.completed","item":{"id":"item_2","type":"agent_message","text":"done"}}
{"type":"turn.completed","usage":{"input_tokens":1000,"cached_input_tokens":900,"cache_write_input_tokens":0,"output_tokens":20,"reasoning_output_tokens":0}}
`
	if out != want || errs != "" {
		t.Errorf("--json printed\n%s\nwant\n%s\nstderr %q", out, want, errs)
	}

	// A failed turn fails the run.
	var o bytes.Buffer
	c := &codexOut{r: workRun{kind: "codex", json: true}, stdout: &o, stderr: &o}
	c.line(nil, event.Status{Busy: true})
	c.line(nil, event.TurnEnd{Reason: "error", Err: "boom"})
	if code := c.finish(); code != 1 || !strings.Contains(o.String(), `{"type":"turn.failed","error":{"message":"boom"}}`) {
		t.Errorf("failed turn: exit %d, %s", code, o.String())
	}
	// and leaves -o as it was, as exec does.
	last = filepath.Join(t.TempDir(), "last")
	c = &codexOut{r: workRun{kind: "codex", lastMsg: last}, stdout: &o, stderr: &o}
	c.line(nil, event.TurnEnd{Reason: "error", Err: "boom"})
	c.finish()
	if _, err := os.Stat(last); err == nil {
		t.Error("a failed run wrote -o")
	}
}

// vibe takes its model from a variable set before it.
func TestParseRunModelFromVariable(t *testing.T) {
	t.Setenv("VIBE_ACTIVE_MODEL", "devstral")
	if got, ok := parseRun("vibe", []string{"-p", "hi"}); !ok || got.model != "devstral" {
		t.Errorf("got %+v %v", got, ok)
	}
}

// A one-shot run prints its last answer, or why the turn failed.
func TestOnceOut(t *testing.T) {
	var out, errb bytes.Buffer
	o := &onceOut{stdout: &out, stderr: &errb}
	o.line(nil, event.Message{Role: "user", Parts: []event.Part{{Kind: event.Text, Text: "hi"}}})
	o.line(nil, event.Message{Role: "assistant", Parts: []event.Part{{Kind: event.Text, Text: "Looking."}}})
	o.line(nil, event.Message{Role: "assistant", Parts: []event.Part{{Kind: event.Text, Text: "Hello"}}})
	o.line(nil, event.TurnEnd{Reason: "done"})
	if code := o.finish(); code != 0 || out.String() != "Hello\n" || errb.Len() > 0 {
		t.Errorf("exit %d, out %q, err %q", code, out.String(), errb.String())
	}
	o.line(nil, event.TurnEnd{Reason: "error", Err: "no key"})
	if code := o.finish(); code != 1 || errb.String() != "Error: no key\n" {
		t.Errorf("failed turn: exit %d, err %q", code, errb.String())
	}
}

// claude -p's output: the result's text, the result, or Claude Code's own
// lines, without the host's.
func TestClaudeOut(t *testing.T) {
	lines := []string{
		`{"type":"system","subtype":"init","session_id":"s1"}`,
		`{"info":{"id":"x","state":"working"},"type":"agtop_info"}`,
		`{"agtop_sent":true,"message":{"content":"hi","role":"user"},"type":"user"}`,
		`{"type":"control_response","response":{}}`,
		`{"type":"stream_event","event":{}}`,
		`{"type":"system","subtype":"status","status":"requesting"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Hello"}]}}`,
		`{"duration_ms":5,"type":"result","is_error":false,"result":"Hello"}`,
	}
	feed := func(r workRun) (string, int) {
		var out bytes.Buffer
		o := &claudeOut{r: r, stdout: &out}
		for _, l := range lines {
			o.line([]byte(l), nil)
		}
		code := o.finish()
		return out.String(), code
	}
	if out, code := feed(workRun{format: "text"}); out != "Hello\n" || code != 0 {
		t.Errorf("text: %q %d", out, code)
	}
	if out, _ := feed(workRun{format: "json"}); out != lines[7]+"\n" {
		t.Errorf("json: %q", out)
	}
	want := strings.Join([]string{lines[0], lines[6], lines[7]}, "\n") + "\n"
	if out, _ := feed(workRun{format: "stream-json"}); out != want {
		t.Errorf("stream-json:\n%s\nwant\n%s", out, want)
	}
	want = strings.Join([]string{lines[0], lines[4], lines[5], lines[6], lines[7]}, "\n") + "\n"
	if out, _ := feed(workRun{format: "stream-json", partial: true}); out != want {
		t.Errorf("stream-json with partials:\n%s\nwant\n%s", out, want)
	}
	lines[7] = `{"type":"result","is_error":true,"result":"API Error"}`
	if out, code := feed(workRun{format: "text"}); out != "API Error\n" || code != 1 {
		t.Errorf("error: %q %d", out, code)
	}
}

// A prompt fed on stdin is put together with the one given as its
// program does: claude on a line after it, codex in a <stdin> block.
func TestWithStdin(t *testing.T) {
	cases := []struct {
		kind, prompt, in, want string
		ok                     bool
	}{
		{"claude", "", "say hi\n", "say hi\n", true},
		{"claude", "do this", "with this", "do this\nwith this", true},
		{"claude", "", "  \n", "", false}, // claude says there's no prompt
		{"codex", "", "  say hi\n\n", "  say hi\n\n", true},
		{"codex", "do this", "a\nb\n\n", "do this\n\n<stdin>\na\nb\n\n</stdin>", true},
		{"codex", "do this", "a", "do this\n\n<stdin>\na\n</stdin>", true},
		{"vibe", "", "say hi", "", false},
	}
	for _, c := range cases {
		got, ok := withStdin(workRun{kind: agent.Kind(c.kind), prompt: c.prompt}, []byte(c.in))
		if got != c.want || ok != c.ok {
			t.Errorf("%s %q + %q = %q %v, want %q %v", c.kind, c.prompt, c.in, got, ok, c.want, c.ok)
		}
	}
	if _, ok := withStdin(workRun{kind: "claude"}, bytes.Repeat([]byte("a"), maxStdin)); ok {
		t.Error("a prompt past what's read is put together")
	}
}

// Only a provider riding the program's harness runs in its place.
func TestRiderOf(t *testing.T) {
	for _, c := range []struct{ k, prog, want agent.Kind }{
		{"ollama", "claude", "ollama"}, {"ollama-codex", "codex", "ollama-codex"},
		{"ollama", "codex", ""}, {"claude", "claude", ""}, {"nope", "claude", ""}, {"", "claude", ""},
	} {
		if got := riderOf(c.k, c.prog); got != c.want {
			t.Errorf("RUSH_AGENT=%s %s rides as %q, want %q", c.k, c.prog, got, c.want)
		}
	}
}

// A Claude run started from a Claude session's shell takes the session's
// permission mode; another agent's session gives it none.
func TestParentMode(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	d := filepath.Join(host.Root(), "p1")
	os.MkdirAll(d, 0o700)
	os.WriteFile(filepath.Join(d, "config.json"), []byte(`{"id":"p1","kind":"claude"}`), 0o600)
	os.WriteFile(filepath.Join(d, "info.json"), []byte(`{"id":"p1","permissionMode":"auto"}`), 0o600)
	t.Setenv("RUSH_SESSION", "p1")
	if got := parentMode("claude"); got != "auto" {
		t.Errorf("claude from a claude session: %q", got)
	}
	if got := parentMode("codex"); got != "" {
		t.Errorf("codex from a claude session: %q", got)
	}
	t.Setenv("RUSH_SESSION", "")
	if got := parentMode("claude"); got != "" {
		t.Errorf("outside rush: %q", got)
	}
}
