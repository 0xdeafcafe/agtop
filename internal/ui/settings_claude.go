package ui

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/claude"
)

// Claude Code's own sections of its page: how agtop runs it, the agent
// definitions its sessions can start as (--agent), and what Claude Code
// itself reads: settings.json (defaults, permissions, hooks) and its env
// block (the variables every session starts with, subagents included).

func init() { agentExtras[loginsKind] = (*Model).claudeSections }

func (m *Model) claudeSections() []section {
	return []section{m.claudeRunSection(), m.claudeAgentsSection(), m.claudeSettingsSection(), m.claudeEnvSection()}
}

func (m *Model) claudeRunSection() section {
	cfg := &m.store.Config
	d := &cfg.Dispatch
	runIn := choiceSetting("Run new sessions in", d.RunIn,
		"Where new Claude Code sessions run. agtop mode runs Claude Code headless in agtop's own host and draws the conversation here; the daemon is Claude Code's own background service and its terminal screen.",
		[][2]string{
			{"", "agtop's conversation view, queue, approvals and overview. /agtop moves a daemon session over."},
			{"daemon", "Claude Code's background service, shown through its own terminal screen."},
		}, func(v string) { d.RunIn = v })
	runIn.unset = "agtop mode"
	onLimit := choiceSetting("When a usage limit hits", d.OnLimit,
		"What an agtop-mode session does when a 5-hour or weekly usage limit stops it.",
		[][2]string{
			{"", "it asks once whether to continue by itself when the limit resets."},
			{"auto", "every session continues by itself at the reset, a few seconds apart."},
			{"off", "sessions wait for you after a limit."},
		}, func(v string) { d.OnLimit = v })
	onLimit.unset = "ask each session"
	quick := choiceSetting("Quick start", onOffWord(d.Lean),
		"Starts agtop-mode sessions without Claude Code's non-essential network traffic (CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC), for them alone. Claude Code is ready in about 0.3s instead of 0.65s, which you feel on every new session and every message after one has rested.",
		[][2]string{
			{"off", "sessions start with everything Claude Code has."},
			{"on", "quicker starts, but no DesignSync, Projects, plugin downloads or live preview in them. Your telemetry settings are unaffected."},
		}, func(v string) { d.Lean = v == "on" })
	compress := choiceSetting("Compress idle transcripts", onOffWord(!cfg.KeepTranscriptsPlain),
		"Transcripts untouched for two days are stored compressed the way macOS stores its own system files: same names, same contents, and Claude Code, grep, your editor and agtop read them exactly as before; the system decompresses as they're read (about 30 ms for a 33 MB one). A transcript written to again is stored plainly again. Each is checked byte for byte before it replaces the original.",
		[][2]string{
			{"on", "about a quarter of the disk they took (33 MB → 8 MB for the biggest)."},
			{"off", "transcripts stay as Claude Code writes them."},
		}, func(v string) { cfg.KeepTranscriptsPlain = v == "off" })
	return section{title: "How agtop runs it", rows: []setting{runIn, onLimit, quick, compress}}
}

// agentDef is one of Claude Code's agent definitions.
type agentDef struct {
	name, scope, model, desc, path string
}

// agentDirs are the user's agents plus the project agents of every
// repository an agent has worked in.
func (m *Model) agentDirs() [][2]string {
	acct := m.store.Config.ActiveAccount()
	dirs := [][2]string{{"user", filepath.Join(acct.ConfigDir, "agents")}}
	seen := map[string]bool{}
	roots := []string{m.launchDir}
	for _, a := range m.snap.Agents {
		roots = append(roots, a.Repo)
	}
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
	return dirs
}

func (m *Model) agentDefs() []agentDef {
	out := []agentDef{{name: "claude", scope: "built-in", desc: "Claude Code's default agent"}}
	for _, d := range m.agentDirs() {
		files, _ := filepath.Glob(filepath.Join(d[1], "*.md"))
		for _, f := range files {
			def := readAgentDef(f)
			def.scope = d[0]
			out = append(out, def)
		}
	}
	return out
}

func readAgentDef(path string) agentDef {
	def := agentDef{name: strings.TrimSuffix(filepath.Base(path), ".md"), path: path}
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
			def.name, inDesc = strings.TrimSpace(strings.TrimPrefix(l, "name:")), false
		case strings.HasPrefix(l, "model:"):
			def.model, inDesc = strings.TrimSpace(strings.TrimPrefix(l, "model:")), false
		case strings.HasPrefix(l, "description:"):
			v := strings.TrimSpace(strings.TrimPrefix(l, "description:"))
			inDesc = v == "|" || v == ">" || v == ""
			if !inDesc {
				def.desc = strings.Trim(v, `"'`)
			}
		case inDesc && strings.HasPrefix(l, " "):
			if def.desc == "" {
				def.desc = strings.TrimSpace(l)
			}
		default:
			inDesc = false
		}
	}
	return def
}

const agentTemplate = `---
name: %s
description: When to use this agent, in one or two sentences.
model: inherit
---

What this agent does, and how.
`

// claudeAgentsSection lists the agent definitions new sessions can start
// as, the one they do marked.
func (m *Model) claudeAgentsSection() section {
	d := m.dialog
	cfg := &m.store.Config
	sec := section{title: "Agent", note: "what new sessions start as (--agent)"}
	newAgent := func() {
		m.ask("new agent name", "", func(v string) tea.Cmd {
			name := strings.ToLower(strings.Join(strings.Fields(v), "-"))
			dir := m.agentDirs()[0][1]
			path := filepath.Join(dir, name+".md")
			if _, err := os.Stat(path); err != nil {
				_ = os.MkdirAll(dir, 0o755)
				_ = os.WriteFile(path, []byte(fmt.Sprintf(agentTemplate, name)), 0o644)
			}
			return editFile(path)
		})
	}
	for _, a := range d.agents {
		used := a.name == cfg.Dispatch.Agent || cfg.Dispatch.Agent == "" && a.scope == "built-in"
		sec.rows = append(sec.rows, setting{
			label: a.name,
			line: func(w int) string {
				mark := faint("○")
				if used {
					mark = paint(cOrange, "●")
				}
				model := ""
				if a.model != "" && a.model != "inherit" {
					model = " · " + strings.TrimPrefix(a.model, "claude-")
				}
				return mark + " " + paint(cText, fit(a.name, 22)) + faint(fit(a.scope+model, 24)) + dim(fit(a.desc, max(10, w-48)))
			},
			key: func(s string) (tea.Cmd, bool) {
				switch s {
				case "enter":
					name := a.name
					if a.scope == "built-in" {
						name = ""
					}
					cfg.Dispatch.Agent = name
					_ = m.store.SaveConfig()
					m.flash("new sessions start as "+a.name, false)
				case "e":
					if a.path != "" {
						return editFile(a.path), true
					}
				case "n":
					newAgent()
				case "x", "d":
					if a.path != "" {
						m.confirmThen("Delete the agent "+a.name+" ("+tildify(a.path)+")?", func() tea.Cmd {
							_ = os.Remove(a.path)
							m.loadDialog()
							d.cursor = max(0, d.cursor-1)
							return nil
						})
					}
				default:
					return nil, false
				}
				return nil, true
			},
			keys: []string{"enter", "start as it", "e", "edit", "n", "new", "d", "delete"},
			about: func() (string, string, string) {
				meta := a.scope
				if a.model != "" {
					meta += " · model " + a.model
				}
				if a.path != "" {
					meta += " · " + tildify(a.path)
				}
				now := "enter makes new sessions start as it."
				if used {
					now = "New sessions start as it."
				}
				return a.name, meta + ". " + a.desc, now
			},
		})
	}
	return sec
}

func (m *Model) claudeSettings() *claude.Settings {
	d := m.dialog
	home := m.store.Config.ActiveAccount() // ~/.claude: every session runs there
	if d.claude == nil || d.claude.Path != home.ConfigDir+"/settings.json" {
		s, err := claude.LoadSettings(home)
		if err != nil {
			m.flash("couldn't read settings.json: "+err.Error(), true)
			s, _ = claude.LoadSettings(claude.Account{ConfigDir: home.ConfigDir + "/.agtop-unreadable"})
		}
		d.claude = s
	}
	return d.claude
}

// saveClaude writes settings.json, saying so when it can't.
func (m *Model) saveClaude(s *claude.Settings) bool {
	if err := s.Save(); err != nil {
		m.flash("couldn't save settings.json: "+err.Error(), true)
		return false
	}
	return true
}

// openSettingsKey is e on any settings.json or env row.
func (m *Model) openSettingsKey(s string) (tea.Cmd, bool) {
	if s == "e" {
		return editFile(m.claudeSettings().Path), true
	}
	return nil, false
}

func (m *Model) claudeSettingsSection() section {
	s := m.claudeSettings()
	unsetNote := "not set: Claude Code's own default."
	json := func(label, key, what string, choices ...string) setting {
		st := setting{label: label, what: what + " (settings.json " + key + ")", choices: choices, unset: "not set",
			means: map[string]string{"": unsetNote}, key: m.openSettingsKey, keys: []string{"e", "open settings.json"}}
		return st
	}
	str := func(label, key, what string, choices ...string) setting {
		st := json(label, key, what, append([]string{""}, choices...)...)
		st.value = s.String(key)
		st.set = func(v string) {
			if v == "" {
				_ = s.Set(key, nil)
			} else {
				_ = s.Set(key, v)
			}
			m.saveClaude(s)
		}
		return st
	}
	flag := func(label, key, what string, invert bool) setting {
		st := json(label, key, what, "", "on", "off")
		var b bool
		if s.Get(key, &b) {
			st.value = onOffWord(b != invert)
		}
		st.set = func(v string) {
			if v == "" {
				_ = s.Set(key, nil)
			} else {
				_ = s.Set(key, (v == "on") != invert)
			}
			m.saveClaude(s)
		}
		return st
	}
	keep := json("Keep transcripts for", "cleanupPeriodDays", "How many days Claude Code keeps old transcripts before cleaning them up", "", "7", "30", "90", "365")
	var days int
	if s.Get("cleanupPeriodDays", &days) {
		keep.value = strconv.Itoa(days)
		keep.means[keep.value] = "older transcripts are removed."
	}
	keep.set = func(v string) {
		if n, err := strconv.Atoi(v); err == nil {
			_ = s.Set("cleanupPeriodDays", n)
		} else {
			_ = s.Set("cleanupPeriodDays", nil)
		}
		m.saveClaude(s)
	}
	models := []string{"opus", "opus[1m]", "sonnet", "haiku", "fable"}
	return section{title: "settings.json", note: tildify(s.Path) + " · every Claude Code session reads it, not only agtop's", rows: []setting{
		str("Default model", "model", "The model every session on this account starts with, unless a session or agtop picks one", models...),
		str("Default effort", "effortLevel", "How hard sessions think by default", "low", "medium", "high", "xhigh", "max"),
		str("Permission mode", "permissions.defaultMode", "What sessions may do without asking. Your allow and deny rules are kept", "default", "acceptEdits", "plan", "auto"),
		flag("Always think", "alwaysThinkingEnabled", "Extended thinking on every request", false),
		flag("Co-authored-by in commits", "includeCoAuthoredBy", "Whether Claude adds a Co-authored-by line to commits it makes", false),
		keep,
		flag("Hooks", "disableAllHooks", "Whether the hooks in your settings run. Turning them off stops every hook without deleting them", true),
	}}
}

// knownEnv are the env vars offered as choices; anything else in env
// shows as a row of its own underneath.
var knownEnv = []struct {
	label, name, what string
	choices           []string
	means             map[string]string
}{
	{"Subagent model", "CLAUDE_CODE_SUBAGENT_MODEL", "The model subagents use, whatever the main session runs", []string{"", "haiku", "sonnet", "opus"}, nil},
	{"Max output tokens", "CLAUDE_CODE_MAX_OUTPUT_TOKENS", "The longest single reply Claude may write", []string{"", "32000", "64000", "128000"}, nil},
	{"Bash timeout", "BASH_DEFAULT_TIMEOUT_MS", "How long a shell command may run before it's stopped", []string{"", "120000", "300000", "600000"},
		map[string]string{"120000": "2 minutes.", "300000": "5 minutes.", "600000": "10 minutes."}},
	{"Telemetry", "DISABLE_TELEMETRY", "Whether Claude Code sends usage telemetry", []string{"", "1"}, map[string]string{"1": "off: no telemetry is sent."}},
	{"Non-essential traffic", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "Whether Claude Code makes non-essential network calls such as update checks", []string{"", "1"},
		map[string]string{"1": "off: only the calls a session needs."}},
}

func (m *Model) claudeEnvSection() section {
	s := m.claudeSettings()
	env := s.Env()
	sec := section{title: "Environment", note: "every session on this account starts with these, subagents included"}
	setEnv := func(name, v string) {
		_ = s.SetEnv(name, v)
		if m.saveClaude(s) {
			m.flash("saved to "+tildify(s.Path), false)
		}
	}
	known := map[string]bool{}
	for _, k := range knownEnv {
		known[k.name] = true
		means := map[string]string{"": "not set: Claude Code's own default."}
		for v, w := range k.means {
			means[v] = w
		}
		sec.rows = append(sec.rows, setting{label: k.label, value: env[k.name], choices: k.choices, unset: "not set",
			what: k.what + " (env " + k.name + ").", means: means,
			set: func(v string) { setEnv(k.name, v) }, key: m.openSettingsKey, keys: []string{"e", "open settings.json"}})
	}
	var extra []string
	for name := range env {
		if !known[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		value := env[name]
		sec.rows = append(sec.rows, setting{label: name, value: value, typed: true,
			what: "An environment variable in this account's settings.json env block. Every Claude session on the account starts with it.",
			set:  func(v string) { setEnv(name, v) },
			key: func(k string) (tea.Cmd, bool) {
				switch k {
				case "x", "delete", "backspace":
					if key := "env:" + name; m.armed != key || time.Since(m.armedAt) > 5*time.Second {
						m.armed, m.armedAt = key, time.Now()
						m.flash("press "+k+" again to remove "+name, false)
						return nil, true
					}
					m.armed = ""
					setEnv(name, "")
					m.flash("removed "+name, false)
					return nil, true
				}
				return m.openSettingsKey(k)
			},
			keys: []string{"x", "remove it", "e", "open settings.json"},
		})
	}
	addVar := func() {
		m.ask("NAME=value", "", func(v string) tea.Cmd {
			name, value, ok := strings.Cut(v, "=")
			name = strings.TrimSpace(name)
			if !ok || name == "" || strings.ContainsAny(name, " \t") {
				m.flash("type it as NAME=value", true)
				return nil
			}
			setEnv(name, strings.TrimSpace(value))
			return nil
		})
	}
	sec.rows = append(sec.rows, setting{label: "+ add a variable",
		line: func(int) string { return faint("+ add a variable") },
		key: func(k string) (tea.Cmd, bool) {
			if k == "enter" || k == "right" || k == "a" {
				addVar()
				return nil, true
			}
			return m.openSettingsKey(k)
		},
		keys: []string{"enter", "add NAME=value"},
		about: func() (string, string, string) {
			return "Add a variable", "Adds an environment variable to this account's settings.json env block. Every Claude session on the account starts with it, subagents included.", "Press enter and type NAME=value."
		},
	})
	return sec
}
