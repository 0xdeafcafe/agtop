package ui

import (
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/settingsfile"
)

// What an agent itself reads, on its page: the keys of its settings file
// its adapter offers (defaults, permissions, hooks), its env block (the
// variables every session starts with, subagents included), and the
// definitions its sessions can start as.

// agentProfile is the profile k's page is about: the one its sessions run
// in.
func (m *Model) agentProfile(k agent.Kind) (agent.Profile, bool) {
	ad, ok := agent.Get(k)
	if !ok {
		return agent.Profile{}, false
	}
	return m.profileOf(ad)
}

// agentDefs are the definitions k's sessions can start as, from its own
// folder and every repository an agent has worked in.
func (m *Model) agentDefs(k agent.Kind) []agent.AgentDef {
	def, ok := agent.As[agent.Definer](k)
	if !ok {
		return nil
	}
	p, _ := m.agentProfile(k)
	roots := []string{m.launchDir}
	for _, a := range m.snap.Agents {
		roots = append(roots, a.Repo)
	}
	return def.AgentDefs(p, roots)
}

// definitionsSection lists the definitions k's new sessions can start as,
// the one they do marked; none if k has none.
func (m *Model) definitionsSection(k agent.Kind) (section, bool) {
	d := m.dialog
	def, ok := agent.As[agent.Definer](k)
	if !ok {
		return section{}, false
	}
	sec := section{title: "Agent definitions", advanced: true, note: "what new sessions start as (--agent)"}
	newAgent := func() {
		m.ask("new agent name", "", func(v string) tea.Cmd {
			name := strings.ToLower(strings.Join(strings.Fields(v), "-"))
			p, _ := m.agentProfile(k)
			path, err := def.NewAgentDef(p, name)
			if err != nil {
				m.flash("couldn't make "+name+": "+err.Error(), true)
				return nil
			}
			return editFile(path)
		})
	}
	for _, a := range d.agents {
		sec.rows = append(sec.rows, m.definitionRow(a, newAgent))
	}
	return sec, true
}

// definitionRow is one definition: enter starts new sessions as it.
func (m *Model) definitionRow(a agent.AgentDef, newAgent func()) setting {
	d := m.dialog
	cfg := &m.store.Config
	used := a.Name == cfg.Dispatch.Agent || cfg.Dispatch.Agent == "" && a.Path == ""
	return setting{
		label: a.Name,
		line: func(w int) string {
			mark := faint("○")
			if used {
				mark = paint(cOrange, "●")
			}
			model := ""
			if a.Model != "" && a.Model != "inherit" {
				model = " · " + strings.TrimPrefix(a.Model, "claude-")
			}
			return mark + " " + paint(cText, fit(a.Name, 22)) + faint(fit(a.Scope+model, 24)) + dim(fit(a.Desc, max(10, w-48)))
		},
		key: func(s string) (tea.Cmd, bool) {
			switch s {
			case "enter":
				name := a.Name
				if a.Path == "" {
					name = ""
				}
				cfg.Dispatch.Agent = name
				_ = m.store.SaveConfig()
				m.flash("new sessions start as "+a.Name, false)
			case "e":
				if a.Path != "" {
					return editFile(a.Path), true
				}
			case "n":
				newAgent()
			case "x", "d":
				if a.Path != "" {
					m.confirmThen("Delete the agent "+a.Name+" ("+tildify(a.Path)+")?", func() tea.Cmd {
						_ = os.Remove(a.Path)
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
			meta := a.Scope
			if a.Model != "" {
				meta += " · model " + a.Model
			}
			if a.Path != "" {
				meta += " · " + tildify(a.Path)
			}
			now := "enter makes new sessions start as it."
			if used {
				now = "New sessions start as it."
			}
			return a.Name, meta + ". " + a.Desc, now
		},
	}
}

// fileSections are what k itself reads, if its adapter says: its settings
// file, then its env block.
func (m *Model) fileSections(k agent.Kind) []section {
	pg, ok := agent.As[agent.SettingsPager](k)
	if !ok || !agent.Supports(k, agent.FeatureSettings) {
		return nil
	}
	s := m.agentSettings(k)
	if s == nil {
		return nil
	}
	page := pg.SettingsPage()
	secs := []section{m.settingsFileSection(page, s)}
	if page.EnvKey != "" {
		secs = append(secs, m.envSection(k, page, s))
	}
	return secs
}

// agentSettings is k's own settings file, read once for the page.
func (m *Model) agentSettings(k agent.Kind) *settingsfile.File {
	d := m.dialog
	p, _ := m.agentProfile(k)
	files := settingsFiles(k, p, "")
	if len(files) == 0 {
		return nil
	}
	path := files[0].path
	if d.settings == nil || d.settings.Path != path {
		s, err := settingsfile.Load(path)
		if err != nil {
			m.flash("couldn't read "+tildify(path)+": "+err.Error(), true)
			s, _ = settingsfile.Load(filepath.Join(filepath.Dir(path), ".agtop-unreadable", filepath.Base(path)))
		}
		d.settings = s
	}
	return d.settings
}

// saveSettings writes the settings file, saying so when it can't.
func (m *Model) saveSettings(s *settingsfile.File) bool {
	if err := s.Save(); err != nil {
		m.flash("couldn't save "+filepath.Base(s.Path)+": "+err.Error(), true)
		return false
	}
	return true
}

// openSettingsKey is e on any settings file or env row.
func (m *Model) openSettingsKey(s string) (tea.Cmd, bool) {
	if s == "e" && m.dialog.settings != nil {
		return editFile(m.dialog.settings.Path), true
	}
	return nil, false
}

func (m *Model) settingsFileSection(page agent.SettingsPage, s *settingsfile.File) section {
	file := filepath.Base(s.Path)
	unsetNote := "not set: " + page.Unset + "."
	row := func(r agent.SettingRow, choices ...string) setting {
		return setting{label: r.Label, what: r.What + " (" + file + " " + r.Key + ")", choices: choices, unset: "not set",
			means: map[string]string{"": unsetNote}, key: m.openSettingsKey, keys: []string{"e", "open " + file}}
	}
	save := func(key string, v any) {
		_ = s.Set(key, v)
		m.saveSettings(s)
	}
	rows := make([]setting, 0, len(page.Rows))
	for _, r := range page.Rows {
		key := r.Key
		var st setting
		switch r.Type {
		case agent.SettingFlag, agent.SettingOff:
			invert := r.Type == agent.SettingOff
			st = row(r, "", "on", "off")
			var b bool
			if s.Get(key, &b) {
				st.value = onOffWord(b != invert)
			}
			st.set = func(v string) {
				if v == "" {
					save(key, nil)
				} else {
					save(key, (v == "on") != invert)
				}
			}
		case agent.SettingInt:
			st = row(r, append([]string{""}, r.Choices...)...)
			var n int
			if s.Get(key, &n) {
				st.value = strconv.Itoa(n)
			}
			st.set = func(v string) {
				if n, err := strconv.Atoi(v); err == nil {
					save(key, n)
				} else {
					save(key, nil)
				}
			}
		default:
			st = row(r, append([]string{""}, r.Choices...)...)
			st.value = s.String(key)
			st.set = func(v string) {
				if v == "" {
					save(key, nil)
				} else {
					save(key, v)
				}
			}
		}
		rows = append(rows, st)
	}
	return section{title: file, advanced: true, note: tildify(s.Path) + " · " + page.Note, rows: rows}
}

func (m *Model) envSection(k agent.Kind, page agent.SettingsPage, s *settingsfile.File) section {
	name := agentName(string(k))
	file := filepath.Base(s.Path)
	env := map[string]string{}
	s.Get(page.EnvKey, &env)
	sec := section{title: "Environment", advanced: true, note: "every session on this account starts with these, subagents included"}
	setEnv := func(v, value string) {
		var err error
		if value == "" {
			err = s.Set(page.EnvKey+"."+v, nil)
		} else {
			err = s.Set(page.EnvKey+"."+v, value)
		}
		if err != nil {
			m.flash("couldn't set "+v+": "+err.Error(), true)
			return
		}
		if m.saveSettings(s) {
			m.flash("saved to "+tildify(s.Path), false)
		}
	}
	known := map[string]bool{}
	for _, e := range page.Env {
		known[e.Name] = true
		means := map[string]string{"": "not set: " + page.Unset + "."}
		maps.Copy(means, e.Means)
		sec.rows = append(sec.rows, setting{label: e.Label, value: env[e.Name], choices: append([]string{""}, e.Choices...), unset: "not set",
			what: e.What + " (env " + e.Name + ").", means: means,
			set: func(v string) { setEnv(e.Name, v) }, key: m.openSettingsKey, keys: []string{"e", "open " + file}})
	}
	var extra []string
	for v := range env {
		if !known[v] {
			extra = append(extra, v)
		}
	}
	sort.Strings(extra)
	for _, v := range extra {
		value := env[v]
		sec.rows = append(sec.rows, setting{label: v, value: value, typed: true,
			what: "An environment variable in this account's " + file + " env block. Every " + name + " session on the account starts with it.",
			set:  func(x string) { setEnv(v, x) },
			key: func(k string) (tea.Cmd, bool) {
				switch k {
				case "x", "delete", "backspace":
					if key := "env:" + v; m.armed != key || time.Since(m.armedAt) > 5*time.Second {
						m.armed, m.armedAt = key, time.Now()
						m.flash("press "+k+" again to remove "+v, false)
						return nil, true
					}
					m.armed = ""
					setEnv(v, "")
					m.flash("removed "+v, false)
					return nil, true
				}
				return m.openSettingsKey(k)
			},
			keys: []string{"x", "remove it", "e", "open " + file},
		})
	}
	addVar := func() {
		m.ask("NAME=value", "", func(in string) tea.Cmd {
			v, value, ok := strings.Cut(in, "=")
			v = strings.TrimSpace(v)
			if !ok || v == "" || strings.ContainsAny(v, " \t") {
				m.flash("type it as NAME=value", true)
				return nil
			}
			setEnv(v, strings.TrimSpace(value))
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
			return "Add a variable", "Adds an environment variable to this account's " + file + " env block. Every " + name + " session on the account starts with it, subagents included.", "Press enter and type NAME=value."
		},
	})
	return sec
}
