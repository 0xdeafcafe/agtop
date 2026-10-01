package ollama

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// suggest is the model to pull when there's none that can do the work: a
// mixture of experts with 3B active, quick on a laptop, and trained for
// agents that code.
const suggest = "qwen3.6:35b-a3b-coding"

// leanTools are the only tools a local model is given. Each tool Claude
// Code offers is more of the prompt a local model reads before it
// answers, and more ways to go wrong: with these alone Claude Code's
// prompt is about 4k tokens rather than 14k, and the first answer comes
// in seconds rather than a minute.
const leanTools = "Bash,Read,Edit,Write,Glob,Grep"

// aliases are the models Claude Code names for itself: its titles,
// subagents and summaries. Each goes to the one model, so a session never
// loads a second one beside it, nor reaches Anthropic.
var aliases = []string{
	"ANTHROPIC_MODEL", "ANTHROPIC_SMALL_FAST_MODEL", "CLAUDE_CODE_SUBAGENT_MODEL",
	"ANTHROPIC_DEFAULT_HAIKU_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL",
	"ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_FABLE_MODEL",
}

// tune is how Claude Code runs on model m, served at base: the
// environment that points it at Ollama and fits it to the model, and its
// flags, with the lean ones added unless flags already say otherwise.
func tune(m Model, base string, flags []string) (env, out []string) {
	env = []string{
		"ANTHROPIC_BASE_URL=" + base,
		"ANTHROPIC_AUTH_TOKEN=ollama",
		"ANTHROPIC_API_KEY=", // never the user's own key, sent to Ollama
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
	}
	for _, a := range aliases {
		env = append(env, a+"="+m.Name)
	}
	if !m.Can("thinking") {
		env = append(env, "CLAUDE_CODE_DISABLE_THINKING=1")
	}
	// Claude Code takes a model it doesn't know for 200k tokens, and
	// compacts late for one loaded with less: tell it the real window.
	if n := window(m); n > 0 {
		env = append(env, "CLAUDE_CODE_AUTO_COMPACT_WINDOW="+strconv.Itoa(n),
			"CLAUDE_CODE_MAX_OUTPUT_TOKENS="+strconv.Itoa(min(32000, n/4)))
	}
	out = append([]string(nil), flags...)
	if !has(out, "--tools") {
		tools := leanTools
		if skilled(m) {
			tools += ",Skill"
		}
		out = append(out, "--tools", tools)
	}
	// The date, git status and the like go after the prompt rather than in
	// it, so Ollama's cache of the prompt holds from one session to the next.
	if !has(out, "--exclude-dynamic-system-prompt-sections") {
		out = append(out, "--exclude-dynamic-system-prompt-sections")
	}
	return env, out
}

// skillWindow is the context a model needs to be given skills: their
// list is thousands of tokens more to read before every answer.
// ponytail: one threshold; a per-model setting if it's wrong for one.
const skillWindow = 128 * 1024

// skilled is whether m is big enough to be given Claude Code's skills.
func skilled(m Model) bool { return window(m) >= skillWindow }

// window is the context m has: what the server loaded it with, else what
// it was trained to; 0 when neither is known.
func window(m Model) int {
	if m.Context > 0 {
		return m.Context
	}
	return m.MaxContext
}

func has(flags []string, f string) bool {
	for _, x := range flags {
		if x == f || strings.HasPrefix(x, f+"=") {
			return true
		}
	}
	return false
}

// pick is the model to run: want when it's named, else one already in
// memory, else the likeliest coder the server has. It must call tools,
// which Claude Code does everything with. It's loaded before it returns,
// so what it was loaded with is known and the first turn doesn't wait.
func pick(ctx context.Context, want string) (Model, error) {
	if err := EnsureRunning(ctx); err != nil {
		return Model{}, err
	}
	info, err := choose(ctx, want)
	if err != nil {
		return Model{}, err
	}
	if info.Can("decision") {
		return Model{}, fmt.Errorf("%s is a decision classifier, not a coding model", info.Name)
	}
	if !info.CanCode() {
		return Model{}, fmt.Errorf("%s can't call tools, and a coding agent does all its work with them: try `ollama pull %s`", info.Name, suggest)
	}
	lctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	info, err = configuredModel(lctx, info)
	if err != nil {
		return Model{}, err
	}
	if err := load(lctx, info.Name); err != nil {
		return Model{}, err
	}
	if in, err := loaded(ctx); err == nil {
		if l, ok := in[info.Name]; ok {
			info.Context, info.VRAM = l.Context, l.VRAM
		}
	}
	knownModels.Store(info.Name, info)
	return info, nil
}

func choose(ctx context.Context, want string) (Model, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if want != "" {
		m, err := show(ctx, want)
		if err != nil {
			return Model{}, fmt.Errorf("%w: pull it with `ollama pull %s`", err, want)
		}
		return m, nil
	}
	in, err := loaded(ctx)
	if err != nil {
		return Model{}, err
	}
	all, err := names(ctx)
	if err != nil {
		return Model{}, err
	}
	var models []Model
	for _, n := range all {
		if strings.HasPrefix(n, "rush-context-") {
			continue
		}
		if m, err := show(ctx, n); err == nil && m.CanCode() {
			m.VRAM = in[n].VRAM
			models = append(models, m)
		}
	}
	if len(models) == 0 {
		return Model{}, fmt.Errorf("Ollama has no model that can call tools: try `ollama pull %s`", suggest)
	}
	sort.SliceStable(models, func(i, j int) bool { return rank(models[i]) > rank(models[j]) })
	return models[0], nil
}

// rank is how good a first choice m is: one in memory already costs
// nothing to start, and one made for code does the work better.
func rank(m Model) int {
	r := 0
	if m.VRAM > 0 {
		r += 4
	}
	if strings.Contains(m.Name, "cod") {
		r += 2
	}
	if !m.Can("thinking") || strings.Contains(m.Name, "instruct") {
		r++ // answers without a long think first
	}
	return r
}
