package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/claude"
)

// The Claude tab edits what Claude Code itself reads: each account's
// settings.json (defaults, permissions, hooks) and its env block (the
// variables every session starts with, subagents included), plus agtop's
// own choices for how sessions run.

// claudeRow is one line of the tab: a setting that cycles through choices,
// or an env var you type a value for.
type claudeRow struct {
	section string
	setting
	envVar string // set for a free-typed env var row
	add    bool   // the "add a variable" row
}

// Known env vars the tab offers as choices; anything else in env shows as a
// free row underneath.
var knownEnv = []struct {
	label, name string
	choices     []string
}{
	{"Subagent model", "CLAUDE_CODE_SUBAGENT_MODEL", []string{"", "haiku", "sonnet", "opus"}},
	{"Max output tokens", "CLAUDE_CODE_MAX_OUTPUT_TOKENS", []string{"", "32000", "64000", "128000"}},
	{"Bash timeout", "BASH_DEFAULT_TIMEOUT_MS", []string{"", "120000", "300000", "600000"}},
	{"Telemetry", "DISABLE_TELEMETRY", []string{"", "1"}},
	{"Non-essential traffic", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", []string{"", "1"}},
}

func (m *Model) claudeSettings() *claude.Settings {
	d := m.dialog
	accts := m.store.Config.AllAccounts()
	if d.claudeAcct >= len(accts) {
		d.claudeAcct = 0
	}
	if d.claude == nil || d.claude.Path != accts[d.claudeAcct].ConfigDir+"/settings.json" {
		s, err := claude.LoadSettings(accts[d.claudeAcct])
		if err != nil {
			m.flash("couldn't read settings.json: "+err.Error(), true)
			s, _ = claude.LoadSettings(claude.Account{ConfigDir: m.store.Config.AllAccounts()[0].ConfigDir + "/.agtop-unreadable"})
		}
		d.claude = s
	}
	return d.claude
}

func (m *Model) claudeRows() []claudeRow {
	d := m.dialog
	cfg := &m.store.Config
	accts := cfg.AllAccounts()
	var names []string
	for _, a := range accts {
		names = append(names, a.Name)
	}
	s := m.claudeSettings()
	save := func() {
		if err := s.Save(); err != nil {
			m.flash("couldn't save settings.json: "+err.Error(), true)
		}
	}
	str := func(key string) setting {
		return setting{value: s.String(key), set: func(v string) {
			if v == "" {
				_ = s.Set(key, nil)
			} else {
				_ = s.Set(key, v)
			}
			save()
		}}
	}
	flag := func(key string, invert bool) setting {
		var b bool
		v := ""
		if s.Get(key, &b) {
			v = map[bool]string{true: "on", false: "off"}[b != invert]
		}
		return setting{value: v, set: func(v string) {
			switch v {
			case "":
				_ = s.Set(key, nil)
			default:
				_ = s.Set(key, (v == "on") != invert)
			}
			save()
		}}
	}
	num := func(key string) setting {
		var n int
		v := ""
		if s.Get(key, &n) {
			v = strconv.Itoa(n)
		}
		return setting{value: v, set: func(v string) {
			if n, err := strconv.Atoi(v); err == nil {
				_ = s.Set(key, n)
			} else {
				_ = s.Set(key, nil)
			}
			save()
		}}
	}
	row := func(section, label string, st setting, choices ...string) claudeRow {
		st.label, st.choices = label, choices
		return claudeRow{section: section, setting: st}
	}

	rows := []claudeRow{
		row("", "Account", setting{value: names[d.claudeAcct], set: func(v string) {
			for i, n := range names {
				if n == v {
					d.claudeAcct, d.claude = i, nil
				}
			}
		}}, names...),
		row("agtop", "New sessions run in", setting{value: cfg.Dispatch.RunIn, set: func(v string) {
			cfg.Dispatch.RunIn = v
			_ = m.store.SaveConfig()
		}}, "", "daemon"),
		row("agtop", "When a usage limit hits", setting{value: cfg.Dispatch.OnLimit, set: func(v string) {
			cfg.Dispatch.OnLimit = v
			_ = m.store.SaveConfig()
		}}, "", "auto", "off"),
		row("agtop", "Quick start", setting{value: onOff(cfg.Dispatch.Lean), set: func(v string) {
			cfg.Dispatch.Lean = v == "on"
			_ = m.store.SaveConfig()
		}}, "off", "on"),
		row("settings.json", "Default model", str("model"), "", "opus", "opus[1m]", "sonnet", "haiku", "fable"),
		row("settings.json", "Default effort", str("effortLevel"), "", "low", "medium", "high", "xhigh", "max"),
		row("settings.json", "Permission mode", str("permissions.defaultMode"), "", "default", "acceptEdits", "plan", "auto"),
		row("settings.json", "Always think", flag("alwaysThinkingEnabled", false), "", "on", "off"),
		row("settings.json", "Co-authored-by in commits", flag("includeCoAuthoredBy", false), "", "on", "off"),
		row("settings.json", "Keep transcripts for", num("cleanupPeriodDays"), "", "7", "30", "90", "365"),
		row("settings.json", "Hooks", flag("disableAllHooks", true), "", "on", "off"),
	}
	env := s.Env()
	known := map[string]bool{}
	for _, k := range knownEnv {
		k := k
		known[k.name] = true
		rows = append(rows, row("Environment", k.label, setting{value: env[k.name], set: func(v string) {
			_ = s.SetEnv(k.name, v)
			save()
		}}, k.choices...))
	}
	var extra []string
	for name := range env {
		if !known[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		rows = append(rows, claudeRow{section: "Environment", envVar: name, setting: setting{label: name, value: env[name]}})
	}
	rows = append(rows, claudeRow{section: "Environment", add: true, setting: setting{label: "+ add a variable"}})
	return rows
}

// shownValue is what a row displays for its value.
func shownValue(r claudeRow) string {
	switch {
	case r.add:
		return "a NAME=value"
	case r.envVar != "":
		return r.value
	case r.value != "":
		return r.value
	}
	switch r.label {
	case "New sessions run in":
		return "agtop mode"
	case "When a usage limit hits":
		return "ask each session"
	case "Quick start":
		return "off"
	case "Account":
		return r.value
	}
	return "not set"
}

func (m *Model) claudeBody(w int) []string {
	d := m.dialog
	var out []string
	section := ""
	for i, r := range m.claudeRows() {
		if r.section != section {
			section = r.section
			title := map[string]string{
				"agtop":         "How agtop runs sessions",
				"settings.json": "settings.json · " + tildify(m.claudeSettings().Path),
				"Environment":   "Environment · every session on this account starts with these",
			}[section]
			out = append(out, "", dim(title))
		}
		shown := shownValue(r)
		// One value column for every row, so the › ends line up.
		valueW := max(18, min(40, w-30-20))
		if r.envVar != "" || r.add {
			line := fit(r.label, 30) + faint("‹ ") + paint(cText, fit(shown, valueW)) + faint(" ›")
			if i == d.cursor {
				out = append(out, highlight(paint(cOrange, "▍")+" "+line, w))
			} else {
				out = append(out, "  "+line)
			}
			continue
		}
		out = append(out, m.settingRow(i, r.setting, shown, 30, valueW, w)...)
	}
	out = append(out, m.about(w)...)
	return append(out, "", keysFit(w, "←→", "change", "enter", "edit a value", "a", "add a variable", "x", "remove it", "e", "open settings.json", "esc", "back"))
}

func (m *Model) claudeAbout() (title string, what, now string) {
	rows := m.claudeRows()
	d := m.dialog
	if d.cursor >= len(rows) {
		return "", "", ""
	}
	r := rows[d.cursor]
	switch {
	case r.add:
		return "Add a variable", "Adds an environment variable to this account's settings.json env block. Every Claude session on the account starts with it, subagents included.", "Press a (or enter) and type NAME=value."
	case r.envVar != "":
		return r.envVar, "An environment variable in this account's settings.json env block. Every Claude session on the account starts with it.", r.envVar + "=" + r.value
	}
	what, now = settingHelp(r.label, r.value)
	return r.label, what, now
}

func (m *Model) claudeKey(s string) tea.Cmd {
	d := m.dialog
	rows := m.claudeRows()
	if d.cursor >= len(rows) {
		return nil
	}
	r := rows[d.cursor]
	switch {
	case s == "e":
		return editFile(m.claudeSettings().Path)
	case s == "a" || (r.add && (s == "enter" || s == "right")):
		m.ask("env var", "")
		return nil
	case r.envVar != "" && (s == "enter" || s == "right"):
		m.ask("value for "+r.envVar, r.value)
		return nil
	case r.envVar != "" && (s == "x" || s == "delete" || s == "backspace"):
		if key := "env:" + r.envVar; m.armed != key || time.Since(m.armedAt) > 5*time.Second {
			m.armed, m.armedAt = key, time.Now()
			m.flash("press "+s+" again to remove "+r.envVar, false)
			return nil
		}
		m.armed = ""
		s := m.claudeSettings()
		_ = s.SetEnv(r.envVar, "")
		if err := s.Save(); err != nil {
			m.flash(err.Error(), true)
		} else {
			m.flash("removed "+r.envVar, false)
		}
		return nil
	case r.envVar != "" || r.add:
		return nil
	}
	switch s {
	case "enter", "right", "l", "space":
		cycle(r.setting, 1)
	case "left", "h":
		cycle(r.setting, -1)
	}
	return nil
}

// claudeAnswer finishes a typed value on the Claude tab.
func (m *Model) claudeAnswer(what, v string) {
	s := m.claudeSettings()
	switch {
	case what == "env var":
		name, value, ok := strings.Cut(v, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" || strings.ContainsAny(name, " \t") {
			m.flash("type it as NAME=value", true)
			return
		}
		_ = s.SetEnv(name, strings.TrimSpace(value))
	case strings.HasPrefix(what, "value for "):
		_ = s.SetEnv(strings.TrimPrefix(what, "value for "), v)
	default:
		return
	}
	if err := s.Save(); err != nil {
		m.flash("couldn't save settings.json: "+err.Error(), true)
		return
	}
	m.flash(fmt.Sprintf("saved to %s", tildify(s.Path)), false)
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return ""
}
