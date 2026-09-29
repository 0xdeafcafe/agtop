package convo

import (
	"strings"
	"testing"

	"github.com/0xdeafcafe/agtop/internal/host"

	_ "github.com/0xdeafcafe/agtop/internal/adapters/claude"
	_ "github.com/0xdeafcafe/agtop/internal/adapters/codex"
	_ "github.com/0xdeafcafe/agtop/internal/adapters/copilot"
)

func TestSpawnOf(t *testing.T) {
	for _, c := range []struct {
		cmd                     string
		ok                      bool
		kind, prompt, from, dir string
		model                   string
	}{
		{cmd: `claude -p "fix the flaky test" --model sonnet`, ok: true, kind: "claude", prompt: "fix the flaky test", model: "sonnet"},
		{cmd: `claude --print --output-format json 'say hi'`, ok: true, kind: "claude", prompt: "say hi"},
		{cmd: `cd /work/app && timeout 600 claude -p "review it"`, ok: true, kind: "claude", prompt: "review it", dir: "/work/app"},
		{cmd: "claude -p \"$(cat <<'EOF'\nread the spec\nand plan it\nEOF\n)\"", ok: true, kind: "claude", prompt: "read the spec\nand plan it"},
		{cmd: `echo "summarise the diff" | claude -p`, ok: true, kind: "claude", prompt: "summarise the diff"},
		{cmd: `cat prompt.md | claude -p`, ok: true, kind: "claude", from: "prompt.md"},
		{cmd: `claude -p < prompt.md`, ok: true, kind: "claude", from: "prompt.md"},
		{cmd: `codex exec -m gpt-5 -C /work/x "port it to rust"`, ok: true, kind: "codex", prompt: "port it to rust", model: "gpt-5", dir: "/work/x"},
		{cmd: `codex exec --full-auto "tidy up" 2>&1 | tail -20`, ok: true, kind: "codex", prompt: "tidy up"},
		{cmd: `codex --search exec "look it up"`, ok: true, kind: "codex", prompt: "look it up"},
		{cmd: `codex -m gpt-5 exec "tidy up"`, ok: true, kind: "codex", prompt: "tidy up", model: "gpt-5"},
		{cmd: `copilot -p "explain main.go" --allow-all-tools`, ok: true, kind: "copilot", prompt: "explain main.go"},
		{cmd: `CLAUDE_CONFIG_DIR=~/.claude-2 claude -p hi`, ok: true, kind: "claude", prompt: "hi"},
		{cmd: "nohup codex exec --sandbox read-only - \\\n  < \"$JOB/tmp/l1-prompt.md\" > \"$JOB/l1.log\" 2>&1 &", ok: true, kind: "codex", from: "$JOB/tmp/l1-prompt.md"},
		{cmd: `nohup claude -p "$(cat "$JOB/tmp/port-prompt.md")" --model claude-opus-4-6 > port.log 2>&1 &`, ok: true, kind: "claude", from: "$JOB/tmp/port-prompt.md", model: "claude-opus-4-6"},
		{cmd: `nohup codex exec -m $M -c model_reasoning_effort="$E" "$(cat $T/lanes/$L.txt)" > $T/$L.log 2>&1 < /dev/null &`, ok: true, kind: "codex", from: "$T/lanes/$L.txt", model: "$M"},
		{cmd: `claude -p --model haiku --input-format stream-json --permission-prompts host < in.jsonl > out.jsonl`, ok: true, kind: "claude", from: "in.jsonl", model: "haiku"},
		{cmd: "claude -p <<'EOF'\nwrite the plan\nEOF", ok: true, kind: "claude", prompt: "write the plan"},
		{cmd: "cat > run.sh <<'EOF'\nclaude -p \"hi\"\nEOF\nchmod +x run.sh"},
		{cmd: `ps -ax | grep 'claude -p'`},
		{cmd: `claude --version`},
		{cmd: `claude mcp list`},
		{cmd: `codex login`},
		{cmd: `codex "interactive"`},
		{cmd: `claude`},
		{cmd: `which claude`},
		{cmd: `go test ./...`},
	} {
		sp, ok := SpawnOf(c.cmd)
		if ok != c.ok {
			t.Errorf("%q: spawn %v, want %v", c.cmd, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		if string(sp.Kind) != c.kind || sp.Prompt != c.prompt || sp.From != c.from || sp.Dir != c.dir || sp.Model != c.model {
			t.Errorf("%q: got %+v", c.cmd, sp)
		}
	}
}

// A command that ran another agent draws as that agent, with the steps its
// own session took under it while it runs: the latest few until opened.
func TestSpawnRow(t *testing.T) {
	s := New()
	s.Info.Cwd = "/work/agtop"
	s.Apply(host.Sent{Text: "get a second opinion"}, at(0))
	s.Apply(toolUse("b1", "Bash", map[string]any{"command": `codex exec -m gpt-5 "review the diff for races"`}), at(1))
	child := New()
	child.Model = "gpt-5"
	child.Apply(host.Sent{Text: "review the diff for races"}, at(1))
	for i, f := range []string{"a.go", "b.go", "c.go", "d.go", "e.go", "f.go"} {
		id := string(rune('a' + i))
		child.Apply(toolUse(id, "Read", map[string]any{"file_path": "/work/agtop/" + f}), at(2+i))
		child.Apply(toolResult(id, "…", false, nil), at(2+i))
	}
	st := s.Spawns()
	if len(st) != 1 {
		t.Fatalf("spawns: %d", len(st))
	}
	s.SetChild(st[0], child)
	out := plain(s.Render(Options{Width: 100, Now: at(9)}))
	for _, want := range []string{"Codex  review the diff for races", "6 steps", "⋯ 2 steps before", "c.go", "f.go"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "b.go") {
		t.Errorf("an early step shows before the row is opened:\n%s", out)
	}
}
