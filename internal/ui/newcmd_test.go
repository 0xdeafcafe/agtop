package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	_ "github.com/0xdeafcafe/rush/internal/adapters/claude"
	_ "github.com/0xdeafcafe/rush/internal/adapters/pi"
	"github.com/0xdeafcafe/rush/internal/agent"
)

// newCmdModel is a model whose Codex offers gpt-x and gpt-y, skipping the
// test unless the harnesses it needs run here.
func newCmdModel(t *testing.T, need ...agent.Kind) *Model {
	t.Helper()
	for _, k := range need {
		if !agent.Runs(k) {
			t.Skipf("%s isn't installed here", k)
		}
	}
	m, _ := benchModel(120, 40)
	m.accts.catalogs = &pending[map[string][]agent.Choice]{} // read nothing of this machine's
	m.listed = map[string][]agent.Choice{"codex": {{ID: "gpt-x"}, {ID: "gpt-y"}}}
	return m
}

// #new codex@openai-sub gpt-y high task: each word taken, the rest the
// task, and nothing about it a default.
func TestNewWords(t *testing.T) {
	m := newCmdModel(t, "codex")
	before := m.store.Config
	n, err := m.parseNew("codex@openai-sub gpt-y high fix the flaky test")
	if err != nil {
		t.Fatal(err)
	}
	if o := n.o; o.kind != "codex" || o.billing != "" || o.model != "gpt-y" || o.effort != "high" || n.task != "fix the flaky test" {
		t.Fatalf("read %+v task %q", o, n.task)
	}
	// A word that would be taken goes to the task in quotes.
	if n, _ = m.parseNew(`codex@openai-sub "high risk, plan it"`); n.o.effort != "" || n.task != "high risk, plan it" {
		t.Fatalf("quoted: %+v %q", n.o, n.task)
	}
	// A model alone finds the provider that serves it.
	if n, _ = m.parseNew("gpt-x do it"); n.o.kind != "codex" || n.o.model != "gpt-x" || n.task != "do it" {
		t.Fatalf("model alone: %+v %q", n.o, n.task)
	}
	// With no task it's the next session's, once.
	m.newCommand("codex@openai-sub gpt-y")
	if m.startOver == nil || m.startOver.model != "gpt-y" {
		t.Fatalf("next session: %+v", m.startOver)
	}
	if m.store.Config.DefaultAgent() != before.DefaultAgent() || len(m.store.Config.Dispatch.Starts) != len(before.Dispatch.Starts) {
		t.Fatal("#new changed a default")
	}
	if p := ansi.Strip(m.newPreview("#new codex@openai-sub gpt-y fix it")); !strings.Contains(p, "OpenAI (Codex)") || !strings.Contains(p, "once") {
		t.Errorf("preview %q", p)
	}
}

// What can't run says why, and what to try.
func TestNewRefuses(t *testing.T) {
	m := newCmdModel(t)
	for arg, want := range map[string]string{
		"nosuch@openai-sub task": "no harness called nosuch",
		"codex@nowhere task":     "no provider called nowhere",
		"codex@anthropic-api x":  "Responses API",
		"codex@anthropic-sub x":  "plan runs in Claude Code",
	} {
		if _, err := m.parseNew(arg); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", arg, err, want)
		}
	}
	if p := ansi.Strip(m.newPreview("#new nosuch@x y")); !strings.HasPrefix(p, "✗ ") {
		t.Errorf("an error on the border: %q", p)
	}
	if m.newPreview("#done") != "" || m.newPreview("hello") != "" {
		t.Error("only #new and #with are previewed")
	}
}

// A plan run through a harness that isn't its own is allowed, and said.
func TestNewOnPlan(t *testing.T) {
	m := newCmdModel(t, "pi")
	n, err := m.parseNew("pi@anthropic-sub tidy imports")
	if err != nil {
		t.Fatal(err)
	}
	if n.o.kind != "anthropic-plan-pi" || n.o.billing != "" || len(n.warn) != 1 || !strings.Contains(n.warn[0], "Anthropic plan through Pi") {
		t.Fatalf("%+v warn %v", n.o, n.warn)
	}
}

// #new's words complete: routes, then the route's models and efforts.
func TestNewArgs(t *testing.T) {
	m := newCmdModel(t, "codex")
	names := func(q string) string {
		var out []string
		for _, c := range m.newArgs("new", q) {
			out = append(out, c.Name)
		}
		return strings.Join(out, ",")
	}
	if got := names("codex@openai-s"); !strings.Contains(got, "new codex@openai-sub") {
		t.Errorf("routes: %s", got)
	}
	if got := names("codex@openai-sub "); !strings.Contains(got, "new codex@openai-sub gpt-x") || !strings.Contains(got, "new codex@openai-sub high") {
		t.Errorf("models and efforts: %s", got)
	}
	if got := names("codex@openai-sub fix th"); got != "" {
		t.Errorf("the task gets nothing: %s", got)
	}
}
