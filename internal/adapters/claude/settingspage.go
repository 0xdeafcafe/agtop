package claude

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/0xdeafcafe/agtop/internal/agent"
)

// SettingsPage is what settings.json and its env block offer: the keys
// people change most, and the variables worth a choice.
func (Adapter) SettingsPage() agent.SettingsPage {
	models := []string{"opus", "opus[1m]", "sonnet", "haiku", "fable"}
	return agent.SettingsPage{
		Note:  "every Claude Code session reads it, not only agtop's",
		Unset: "Claude Code's own default",
		Rows: []agent.SettingRow{
			{Label: "Default model", Key: "model", What: "The model every session on this account starts with, unless a session or agtop picks one", Choices: models},
			{Label: "Default effort", Key: "effortLevel", What: "How hard sessions think by default", Choices: []string{"low", "medium", "high", "xhigh", "max"}},
			{Label: "Permission mode", Key: "permissions.defaultMode", What: "What sessions may do without asking. Your allow and deny rules are kept", Choices: []string{"default", "acceptEdits", "plan", "auto"}},
			{Label: "Always think", Key: "alwaysThinkingEnabled", What: "Extended thinking on every request", Type: agent.SettingFlag},
			{Label: "Co-authored-by in commits", Key: "includeCoAuthoredBy", What: "Whether Claude adds a Co-authored-by line to commits it makes", Type: agent.SettingFlag},
			{Label: "Keep transcripts for", Key: "cleanupPeriodDays", What: "How many days Claude Code keeps old transcripts before cleaning them up", Type: agent.SettingInt, Choices: []string{"7", "30", "90", "365"}},
			{Label: "Hooks", Key: "disableAllHooks", What: "Whether the hooks in your settings run. Turning them off stops every hook without deleting them", Type: agent.SettingOff},
		},
		EnvKey: "env",
		Env: []agent.EnvSetting{
			{Label: "Subagent model", Name: "CLAUDE_CODE_SUBAGENT_MODEL", What: "The model subagents use, whatever the main session runs", Choices: []string{"haiku", "sonnet", "opus"}},
			{Label: "Max output tokens", Name: "CLAUDE_CODE_MAX_OUTPUT_TOKENS", What: "The longest single reply Claude may write", Choices: []string{"32000", "64000", "128000"}},
			{Label: "Bash timeout", Name: "BASH_DEFAULT_TIMEOUT_MS", What: "How long a shell command may run before it's stopped", Choices: []string{"120000", "300000", "600000"},
				Means: map[string]string{"120000": "2 minutes.", "300000": "5 minutes.", "600000": "10 minutes."}},
			{Label: "Telemetry", Name: "DISABLE_TELEMETRY", What: "Whether Claude Code sends usage telemetry", Choices: []string{"1"}, Means: map[string]string{"1": "off: no telemetry is sent."}},
			{Label: "Non-essential traffic", Name: "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", What: "Whether Claude Code makes non-essential network calls such as update checks", Choices: []string{"1"},
				Means: map[string]string{"1": "off: only the calls a session needs."}},
		},
	}
}

// AgentDefs are Claude Code's own agent, then the markdown definitions in
// p's agents folder and every project's .claude/agents.
func (Adapter) AgentDefs(p agent.Profile, roots []string) []agent.AgentDef {
	dirs := [][2]string{{"user", filepath.Join(p.Dir, "agents")}}
	seen := map[string]bool{}
	for _, r := range roots {
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		d := filepath.Join(r, ".claude", "agents")
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			dirs = append(dirs, [2]string{filepath.Base(r), d})
		}
	}
	out := []agent.AgentDef{{Name: "claude", Scope: "built-in", Desc: "Claude Code's default agent"}}
	for _, d := range dirs {
		files, _ := filepath.Glob(filepath.Join(d[1], "*.md"))
		for _, f := range files {
			def := readAgentDef(f)
			def.Scope = d[0]
			out = append(out, def)
		}
	}
	return out
}

const agentTemplate = `---
name: %s
description: When to use this agent, in one or two sentences.
model: inherit
---

What this agent does, and how.
`

// NewAgentDef is p's agents/name.md, from a template if it isn't there.
func (Adapter) NewAgentDef(p agent.Profile, name string) (string, error) {
	dir := filepath.Join(p.Dir, "agents")
	path := filepath.Join(dir, name+".md")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return path, err
	}
	return path, os.WriteFile(path, fmt.Appendf(nil, agentTemplate, name), 0o644)
}

// readAgentDef reads a definition's front matter.
func readAgentDef(path string) agent.AgentDef {
	def := agent.AgentDef{Name: strings.TrimSuffix(filepath.Base(path), ".md"), Path: path}
	f, err := os.Open(path)
	if err != nil {
		return def
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	inFront, inDesc := false, false
	for i := 0; sc.Scan() && i < 60; i++ {
		l := sc.Text()
		if strings.TrimSpace(l) == "---" {
			if inFront {
				break
			}
			inFront = true
			continue
		}
		if !inFront {
			continue
		}
		switch {
		case strings.HasPrefix(l, "name:"):
			def.Name, inDesc = strings.TrimSpace(strings.TrimPrefix(l, "name:")), false
		case strings.HasPrefix(l, "model:"):
			def.Model, inDesc = strings.TrimSpace(strings.TrimPrefix(l, "model:")), false
		case strings.HasPrefix(l, "description:"):
			v := strings.TrimSpace(strings.TrimPrefix(l, "description:"))
			inDesc = v == "|" || v == ">" || v == ""
			if !inDesc {
				def.Desc = strings.Trim(v, `"'`)
			}
		case inDesc && strings.HasPrefix(l, " "):
			if def.Desc == "" {
				def.Desc = strings.TrimSpace(l)
			}
		default:
			inDesc = false
		}
	}
	return def
}
