package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// Profiles lists your profiles and the folders that pick one; enter opens
// a profile to change which agents it runs, in what order, and what
// happens when their accounts run low. A profile is agents and a policy,
// not accounts: each agent is signed in to one account at a time, shared
// by all its sessions, and agtop moves it between them itself (Accounts).

var profilesPage = page{
	name: "Profiles",
	head: (*Model).profilesHead,
	form: (*Model).profilesForm,
	pre: func(m *Model, s string) (tea.Cmd, bool) {
		d := m.dialog
		if d.profile != "" && (s == "esc" || s == "q") {
			d.profile, d.cursor = "", 0
			return nil, true
		}
		return nil, false
	},
}

// editing is the profile Profiles has open, if it still exists.
func (m *Model) editing() (state.Profile, bool) {
	name := m.dialog.profile
	if name == "" {
		return state.Profile{}, false
	}
	for _, p := range m.store.Config.Profiles {
		if strings.EqualFold(p.Name, name) {
			return p, true
		}
	}
	return state.Profile{}, false
}

// changeProfile edits the profile called name and saves it.
func (m *Model) changeProfile(name string, f func(*state.Profile)) {
	cfg := &m.store.Config
	for _, p := range cfg.Profiles {
		if strings.EqualFold(p.Name, name) {
			f(&p)
			cfg.SetProfile(name, p)
			_ = m.store.SaveConfig()
			m.spillTo()
			return
		}
	}
}

func (m *Model) profilesHead(w int) []string {
	if p, ok := m.editing(); ok {
		now := dim("New sessions under it start on ")
		if pick, ok := p.Pick(m.room()); ok {
			now += paint(cText, agentName(pick.Kind))
			if pick.Account.Name != "" {
				now += dim(" · ") + paint(cText, pick.Account.Name)
			}
			if pick.Wait {
				now += paint(cYellow, " and wait: every account is nearly out")
			}
		} else {
			now += paint(cYellow, "nothing: none of its agents is installed")
		}
		return []string{"", paint(cText+bold, p.Name) + faint("   esc back to every profile"), now}
	}
	var out []string
	for _, l := range wrap("A profile is which agents new sessions run, in order, and what happens when their accounts run low. A new session gets the profile you pick for it (#profile name), else the one for its folder, else the default ★.", w-2) {
		out = append(out, dim(l))
	}
	for _, l := range wrap("Accounts aren't part of a profile: each agent is signed in to one account at a time for all its sessions, and agtop moves it to another with room by itself. Accounts has them.", w-2) {
		out = append(out, faint(l))
	}
	return append([]string{""}, out...)
}

func (m *Model) profilesForm() []section {
	if p, ok := m.editing(); ok {
		return m.profileForm(p)
	}
	m.dialog.profile = ""
	cfg := &m.store.Config
	def := cfg.Default()
	sec := section{title: "Profiles"}
	for _, p := range cfg.Profiles {
		name := p.Name
		isDef := strings.EqualFold(name, def.Name)
		folders := 0
		for _, r := range cfg.FolderRules {
			if strings.EqualFold(r.Profile, name) {
				folders++
			}
		}
		sec.rows = append(sec.rows, setting{
			label: name,
			line: func(w int) string {
				mark := "  "
				if isDef {
					mark = paint(cOrange, "★ ")
				}
				l := mark + paint(cText+bold, fit(name, 16)) + fit(m.profileAgents(p), 34) + dim(fit(m.profilePolicy(p), max(10, w-60)))
				if folders > 0 {
					l += faint(fmt.Sprintf("  %d folder%s", folders, map[bool]string{true: "", false: "s"}[folders == 1]))
				}
				return l
			},
			key: func(s string) (tea.Cmd, bool) {
				switch s {
				case "enter", "right":
					m.dialog.profile, m.dialog.cursor = name, 0
				case "*", "space":
					cfg.SetDefaultProfile(name)
					_ = m.store.SaveConfig()
					m.spillTo()
					m.flash(name+" is the default: new sessions get it unless you or a folder pick another", false)
				case "r":
					m.renameProfile(name)
				case "d", "x":
					if len(cfg.Profiles) < 2 {
						m.flash("there's always one profile: make another first", true)
						return nil, true
					}
					m.confirmThen("Delete the profile "+name+"? Its folders go back to the default.", func() tea.Cmd {
						cfg.DeleteProfile(name)
						_ = m.store.SaveConfig()
						m.dialog.cursor = max(0, m.dialog.cursor-1)
						return nil
					})
				case "n":
					m.newProfile()
				default:
					return nil, false
				}
				return nil, true
			},
			keys: []string{"enter", "open", "*", "make default", "n", "new", "r", "rename", "d", "delete"},
			about: func() (string, string, string) {
				now := m.profileAgents(p) + ". " + m.profilePolicy(p) + "."
				if isDef {
					now += " It's the default."
				}
				return name, "enter opens it to change its agents, their order, and what happens at a limit.", now
			},
		})
	}
	sec.rows = append(sec.rows, setting{
		label: "+ new profile",
		line:  func(int) string { return faint("+ new profile") },
		key: func(s string) (tea.Cmd, bool) {
			if s == "enter" || s == "right" || s == "n" {
				m.newProfile()
				return nil, true
			}
			return nil, false
		},
		keys: []string{"enter", "name it"},
		about: func() (string, string, string) {
			return "New profile", "A profile of its own for some of your work: only Codex for one client, say, or Claude then Codex for another. It starts with the default's agents, for you to change.", ""
		},
	})
	return []section{sec, m.folderSection("")}
}

// profileAgents are a profile's agents in words, in order.
func (m *Model) profileAgents(p state.Profile) string {
	inst := p.Installed()
	if len(inst) == 0 {
		return "none of its agents is installed"
	}
	var names []string
	for _, k := range inst {
		names = append(names, kindName(agent.Kind(k)))
	}
	return strings.Join(names, " → ")
}

// profilePolicy is what a profile does when accounts run low, in words.
func (m *Model) profilePolicy(p state.Profile) string {
	start := "starts on the first"
	if p.Mixes() && len(p.Installed()) > 1 {
		start = "moves on when it's out"
	}
	return start + " · at a limit: " + limitWords(p.Limit())
}

func (m *Model) newProfile() {
	m.ask("name the new profile", "", func(v string) tea.Cmd {
		cfg := &m.store.Config
		for _, p := range cfg.Profiles {
			if strings.EqualFold(p.Name, v) {
				m.flash("there's a profile called "+p.Name+" already", true)
				return nil
			}
		}
		p := cfg.Default()
		p.Name = v
		p.Providers = slices.Clone(p.Providers)
		cfg.SetProfile("", p)
		_ = m.store.SaveConfig()
		m.dialog.profile, m.dialog.cursor = v, 0
		return nil
	})
}

func (m *Model) renameProfile(name string) {
	m.ask("rename "+name, name, func(v string) tea.Cmd {
		for _, p := range m.store.Config.Profiles {
			if strings.EqualFold(p.Name, v) && !strings.EqualFold(v, name) {
				m.flash("there's a profile called "+p.Name+" already", true)
				return nil
			}
		}
		m.changeProfile(name, func(p *state.Profile) { p.Name = v })
		if m.dialog.profile != "" {
			m.dialog.profile = v
		}
		return nil
	})
}

// profileForm is one profile, open: its agents in order, what it does
// when they run low, and the folders that pick it.
func (m *Model) profileForm(p state.Profile) []section {
	name := p.Name
	change := func(f func(*state.Profile)) { m.changeProfile(name, f) }

	// Its agents first, in its order; then the installed ones it leaves out.
	agents := section{title: "Agents", note: "new sessions start on the first; the rest are where they can move on to"}
	var kinds []string
	for _, k := range p.Providers {
		if agent.Installed(agent.Kind(k)) {
			kinds = append(kinds, k)
		}
	}
	for _, ad := range m.agentOrder() {
		if k := string(ad.Kind()); !slices.Contains(kinds, k) {
			kinds = append(kinds, k)
		}
	}
	used := 0
	for _, k := range kinds {
		in := p.Has(k)
		at := used
		if in {
			used++
		}
		agents.rows = append(agents.rows, setting{
			label: agentName(k),
			line: func(int) string {
				if !in {
					return faint("  ○ ") + glyph(agent.Kind(k)) + " " + dim(fit(agentName(k), 18)) + faint("not used")
				}
				n := paint(cOrange, fmt.Sprintf("%d", at+1))
				if !agent.Runs(agent.Kind(k)) {
					return n + faint(" ● ") + glyph(agent.Kind(k)) + " " + paint(cText, fit(agentName(k), 18)) + paint(cYellow, "agtop can't start its sessions yet")
				}
				return n + paint(cOrange, " ● ") + glyph(agent.Kind(k)) + " " + paint(cText, agentName(k))
			},
			key: func(s string) (tea.Cmd, bool) {
				switch s {
				case "enter", "space":
					change(func(p *state.Profile) {
						if p.Has(k) {
							if len(p.Providers) > 1 {
								p.Providers = slices.DeleteFunc(p.Providers, func(q string) bool { return q == k })
							} else {
								m.flash("a profile needs one agent", true)
							}
						} else {
							p.Providers = append(p.Providers, k)
						}
					})
				case "K", "shift+up", "J", "shift+down":
					d := map[string]int{"K": -1, "shift+up": -1, "J": 1, "shift+down": 1}[s]
					change(func(p *state.Profile) {
						i := slices.Index(p.Providers, k)
						if j := i + d; i >= 0 && j >= 0 && j < len(p.Providers) {
							p.Providers[i], p.Providers[j] = p.Providers[j], p.Providers[i]
							m.dialog.cursor += d
						}
					})
				default:
					return nil, false
				}
				return nil, true
			},
			keys: []string{"enter", "use or not", "J/K", "move"},
			about: func() (string, string, string) {
				what := "The agents this profile's sessions run, in order. New sessions start on the first; the others are where they go when its accounts are out, if the profile moves on or hands off."
				if !in {
					return agentName(k), what, "Not used: enter adds it at the end."
				}
				return agentName(k), what, fmt.Sprintf("Number %d. enter drops it; J and K move it.", at+1)
			},
		})
	}

	mix := choiceSetting("When the first is out", firstNonEmpty(p.Mix, state.MixStay),
		"What new sessions do once every account of the first agent is nearly out. Running sessions stay where they are.",
		[][2]string{
			{state.MixStay, "new sessions still start on the first agent, and wait for its limits to reset."},
			{state.MixMix, "new sessions start on the next agent in the list with room, until the first's accounts reset."},
		}, func(v string) { change(func(p *state.Profile) { p.Mix = v }) })
	mix.names = map[string]string{state.MixStay: "wait for it", state.MixMix: "move on"}
	limit := choiceSetting("When a limit stops a session", p.Limit(),
		"What a running conversation does when a usage limit stops it.",
		[][2]string{
			{state.LimitAccount, "agtop moves its agent to another account with room, and the conversation carries on."},
			{state.LimitHandoff, "another account first; when none has room, the conversation is handed to the next agent in the list, with what it was doing."},
			{state.LimitWait, "it waits for the limit to reset."},
		}, func(v string) { change(func(p *state.Profile) { p.OnLimit = v }) })
	limit.names = map[string]string{state.LimitAccount: "switch account", state.LimitHandoff: "hand off", state.LimitWait: "wait"}
	isDef := strings.EqualFold(m.store.Config.Default().Name, name)
	def := choiceSetting("Default", map[bool]string{true: "yes", false: "no"}[isDef],
		"Whether a new session gets this profile when you haven't picked one and its folder has none.",
		[][2]string{{"yes", "it's the default."}, {"no", "sessions get it when you pick it (#profile " + name + ") or its folder does."}},
		func(v string) {
			if v == "yes" {
				m.store.Config.SetDefaultProfile(name)
				m.spillTo()
			}
		})
	rename := setting{label: "Name", value: name, typed: true, what: "What it's called, for #profile and the top bar.",
		run: func(string) tea.Cmd { return nil }}
	rename.key = func(s string) (tea.Cmd, bool) {
		if s == "enter" || s == "right" {
			m.renameProfile(name)
			return nil, true
		}
		return nil, false
	}
	return []section{
		agents,
		{title: "When accounts run low", rows: []setting{mix, limit}},
		{title: "This profile", rows: []setting{rename, def}},
		m.folderSection(name),
	}
}

// folderSection is the folders that pick a profile: every rule, or only
// profile's.
func (m *Model) folderSection(profile string) section {
	cfg := &m.store.Config
	sec := section{title: "Folders", note: "a session started in one, or a folder inside it, gets its profile"}
	if profile != "" {
		sec.note = "a session started in one, or a folder inside it, gets " + profile
	}
	for _, r := range cfg.FolderRules {
		if profile != "" && !strings.EqualFold(r.Profile, profile) {
			continue
		}
		path, to := r.Path, r.Profile
		sec.rows = append(sec.rows, setting{
			label: path,
			line: func(int) string {
				l := paint(cText, fit(tildify(state.ExpandHome(path)), 40))
				if profile == "" {
					l += faint("→ ") + dim(to)
				}
				return l
			},
			key: func(s string) (tea.Cmd, bool) {
				switch s {
				case "enter", "right":
					if profile == "" {
						m.dialog.profile, m.dialog.cursor = to, 0
					}
				case "x", "d", "backspace":
					cfg.SetRule(path, "")
					_ = m.store.SaveConfig()
					m.dialog.cursor = max(0, m.dialog.cursor-1)
				default:
					return nil, false
				}
				return nil, true
			},
			keys: []string{"x", "remove"},
			about: func() (string, string, string) {
				return tildify(state.ExpandHome(path)), "Sessions started here, or in a folder inside it, get " + to + ", unless you pick another for them. The longest folder that matches wins.", ""
			},
		})
	}
	add := func() {
		// The selected session's folder, else where new sessions start.
		dir := m.startDir()
		if a := m.selected(); a != nil && a.Cwd != "" {
			dir = a.Cwd
		}
		m.ask("folder", tildify(dir), func(v string) tea.Cmd {
			to := profile
			if to == "" {
				to = cfg.Default().Name
			}
			cfg.SetRule(v, to)
			_ = m.store.SaveConfig()
			m.flash(tildify(state.ExpandHome(v))+" gets "+to, false)
			return nil
		})
	}
	sec.rows = append(sec.rows, setting{
		label: "+ add a folder",
		line:  func(int) string { return faint("+ add a folder") },
		key: func(s string) (tea.Cmd, bool) {
			if s == "enter" || s == "right" || s == "a" {
				add()
				return nil, true
			}
			return nil, false
		},
		keys: []string{"enter", "the selected session's folder, or type one"},
		about: func() (string, string, string) {
			what := "Gives a folder a profile, so every session started in it gets that one: a client's repositories on its own account's agent, say."
			if profile == "" {
				what += " It gets the default; open the profile you want to give it another."
			}
			return "Add a folder", what, ""
		},
	})
	return sec
}
