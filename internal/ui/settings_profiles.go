package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// Profiles lists every installed provider, each a profile of its own, then
// the profiles you made, and the folders that pick one. enter opens one: a
// provider's to choose which harness it runs in and what it does at a
// limit; yours to also choose its providers and their order. A profile is
// providers and a policy, not accounts: each provider is signed in to one
// account at a time, shared by all its sessions, and agtop moves it
// between them itself (Accounts).

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
	if m.dialog.profile == "" {
		return state.Profile{}, false
	}
	return m.store.Config.ProfileNamed(m.dialog.profile)
}

// changeProfile edits the profile called name and saves it. A provider's
// own, changed, is kept as one of yours of its name, standing in for it.
func (m *Model) changeProfile(name string, f func(*state.Profile)) {
	cfg := &m.store.Config
	p, ok := cfg.ProfileNamed(name)
	if !ok {
		return
	}
	f(&p)
	cfg.SetProfile(name, p)
	_ = m.store.SaveConfig()
	m.spillTo()
}

// installedProviders are the providers with an agent installed here.
func installedProviders() []string {
	var out []string
	for _, pr := range agent.Providers() {
		if agent.ProviderInstalled(pr) {
			out = append(out, pr)
		}
	}
	return out
}

// ownProfile is whether name is an installed provider's own profile, as
// built in or as you changed it.
func ownProfile(name string) bool {
	return slices.ContainsFunc(installedProviders(), func(pr string) bool { return strings.EqualFold(pr, name) })
}

// runsInWords is " in Pi" when agent k is its provider in another's
// harness; empty when it's its own.
func runsInWords(k agent.Kind) string {
	if h := agent.HarnessOf(k); h != k {
		return " in " + agentName(string(h))
	}
	return ""
}

func (m *Model) profilesHead(w int) []string {
	if p, ok := m.editing(); ok {
		return m.profileHead(p)
	}
	var out []string
	for _, l := range wrap("Every provider you have is a profile of its own: sessions under it run that provider alone. Make your own to group several, Claude then Codex say, and choose what happens when they run low. A new session gets the profile you pick for it (#profile name), else its folder's, else the default ★.", w-2) {
		out = append(out, dim(l))
	}
	for _, l := range wrap("A provider can run in more than one harness: Ollama's models in Claude Code, Pi or Codex. Open a provider to choose which; a profile of yours can choose again for itself.", w-2) {
		out = append(out, faint(l))
	}
	return append([]string{""}, out...)
}

// profileHead is the head of one profile, open: its name, and where new
// sessions under it start now.
func (m *Model) profileHead(p state.Profile) []string {
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
		now += paint(cYellow, "nothing: none of its providers is installed")
	}
	title := paint(cText+bold, p.Name)
	if ownProfile(p.Name) {
		title = providerTag(agent.Kind(p.Name)) + dim("'s own profile")
	}
	return []string{"", title + faint("   esc back to every profile"), now}
}

func (m *Model) profilesForm() []section {
	if p, ok := m.editing(); ok {
		return m.profileForm(p)
	}
	m.dialog.profile = ""
	own := section{title: "Providers", note: "each is a profile of its own"}
	for _, pr := range installedProviders() {
		own.rows = append(own.rows, m.ownProfileRow(pr))
	}
	mine := section{title: "Your profiles", note: "providers grouped, in order"}
	for _, p := range m.store.Config.Profiles {
		if !ownProfile(p.Name) {
			mine.rows = append(mine.rows, m.profileRow(p.Name))
		}
	}
	mine.rows = append(mine.rows, setting{
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
			return "New profile", "A group of providers for some of your work: Claude then Codex for one client, say, or Ollama in Pi for another. It starts with the default's providers, for you to change.", ""
		},
	})
	return []section{own, mine, m.folderSection("")}
}

// profileMark is ★ for the default profile, room for it otherwise.
func (m *Model) profileMark(name string) string {
	if strings.EqualFold(name, m.store.Config.Default().Name) {
		return paint(cOrange, "★ ")
	}
	return "  "
}

// profileFolders says how many folders pick the profile called name.
func (m *Model) profileFolders(name string) string {
	n := 0
	for _, r := range m.store.Config.FolderRules {
		if strings.EqualFold(r.Profile, name) {
			n++
		}
	}
	if n == 0 {
		return ""
	}
	return faint(fmt.Sprintf("  %d folder%s", n, map[bool]string{true: "", false: "s"}[n == 1]))
}

// makeDefaultProfile makes the profile called name the default, and says so.
func (m *Model) makeDefaultProfile(name string) {
	m.store.Config.SetDefaultProfile(name)
	_ = m.store.SaveConfig()
	m.spillTo()
	m.flash(name+" is the default: new sessions get it unless you or a folder pick another", false)
}

// ownProfileRow is provider pr's own profile in the list: the harness it
// runs in, and its policy when you changed it (x puts it back).
func (m *Model) ownProfileRow(pr string) setting {
	cfg := &m.store.Config
	p, _ := cfg.ProfileNamed(pr)
	changed := !p.Builtin
	return setting{
		label: pr,
		line: func(w int) string {
			k := agent.Kind(p.KindOf(pr))
			l := m.profileMark(pr) + fit(providerTag(agent.Kind(pr)), 18) + dim(fit(strings.TrimPrefix(runsInWords(k), " "), 20))
			if changed {
				l += dim(fit(m.profilePolicy(p), max(10, w-44)))
			}
			return l + m.profileFolders(pr)
		},
		key: func(s string) (tea.Cmd, bool) {
			switch s {
			case "enter", "right":
				m.dialog.profile, m.dialog.cursor = pr, 0
			case "*", "space":
				m.makeDefaultProfile(pr)
			case "x", "d":
				if changed {
					cfg.DeleteProfile(pr)
					_ = m.store.SaveConfig()
					m.flash(agentName(pr)+"'s own profile is as it was", false)
				}
			case "n":
				m.newProfile()
			default:
				return nil, false
			}
			return nil, true
		},
		keys: []string{"enter", "open", "*", "make default", "n", "new profile"},
		about: func() (string, string, string) {
			now := "Runs " + agentName(p.KindOf(pr)) + "."
			if changed {
				now += " " + m.profilePolicy(p) + ". x puts it back as it was."
			}
			if strings.EqualFold(pr, cfg.Default().Name) {
				now += " It's the default."
			}
			return agentName(pr), "Sessions under it run " + agentName(pr) + " alone. enter opens it to choose the harness it runs in and what happens at a limit.", now
		},
	}
}

// profileRow is a profile of yours in the list.
func (m *Model) profileRow(name string) setting {
	cfg := &m.store.Config
	return setting{
		label: name,
		line: func(w int) string {
			q, _ := cfg.ProfileNamed(name)
			return m.profileMark(name) + paint(cText+bold, fit(name, 16)) + fit(m.profileAgents(q), 34) + dim(fit(m.profilePolicy(q), max(10, w-60))) + m.profileFolders(name)
		},
		key: func(s string) (tea.Cmd, bool) {
			switch s {
			case "enter", "right":
				m.dialog.profile, m.dialog.cursor = name, 0
			case "*", "space":
				m.makeDefaultProfile(name)
			case "r":
				m.renameProfile(name)
			case "d", "x":
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
			q, _ := cfg.ProfileNamed(name)
			now := m.profileAgents(q) + ". " + m.profilePolicy(q) + "."
			if strings.EqualFold(name, cfg.Default().Name) {
				now += " It's the default."
			}
			return name, "enter opens it to change its providers, their order, where they run, and what happens at a limit.", now
		},
	}
}

// profileAgents are a profile's agents in words, in order.
func (m *Model) profileAgents(p state.Profile) string {
	inst := p.Installed()
	if len(inst) == 0 {
		return "none of its providers is installed"
	}
	var names []string
	for _, k := range inst {
		names = append(names, kindName(agent.Kind(agent.ProviderOf(agent.Kind(k))))+runsInWords(agent.Kind(k)))
	}
	return strings.Join(names, " → ")
}

// profilePolicy is what a profile does when accounts run low, in words.
func (m *Model) profilePolicy(p state.Profile) string {
	start := "starts on the first"
	if len(p.Installed()) < 2 {
		start = "stays on it"
	} else if p.Mixes() {
		start = "moves on when it's out"
	}
	return start + " · at a limit: " + limitWords(p.Limit())
}

func (m *Model) newProfile() {
	m.ask("name the new profile", "", func(v string) tea.Cmd {
		cfg := &m.store.Config
		for _, p := range cfg.AllProfiles() {
			if strings.EqualFold(p.Name, v) {
				m.flash("there's a profile called "+p.Name+" already", true)
				return nil
			}
		}
		if _, ok := agent.Get(agent.Kind(strings.ToLower(v))); ok {
			m.flash(v+" is an agent's name: #profile "+v+" runs it", true)
			return nil
		}
		d := cfg.Default()
		cfg.SetProfile("", state.Profile{Name: v, Providers: slices.Clone(d.Providers), Mix: d.Mix, OnLimit: d.OnLimit})
		_ = m.store.SaveConfig()
		m.dialog.profile, m.dialog.cursor = v, 0
		return nil
	})
}

func (m *Model) renameProfile(name string) {
	m.ask("rename "+name, name, func(v string) tea.Cmd {
		for _, p := range m.store.Config.AllProfiles() {
			if strings.EqualFold(p.Name, v) && !strings.EqualFold(v, name) {
				m.flash("there's a profile called "+p.Name+" already", true)
				return nil
			}
		}
		if _, ok := agent.Get(agent.Kind(strings.ToLower(v))); ok && !strings.EqualFold(v, name) {
			m.flash(v+" is an agent's name: #profile "+v+" runs it", true)
			return nil
		}
		m.changeProfile(name, func(p *state.Profile) { p.Name = v })
		if m.dialog.profile != "" {
			m.dialog.profile = v
		}
		return nil
	})
}

// harnessSetting is where provider pr runs: value is the harness chosen
// ("" the default, when unset is given), set keeps a choice.
func harnessSetting(pr, value, unset, what string, set func(string)) setting {
	var choices [][2]string
	if unset != "" {
		choices = append(choices, [2]string{"", unset})
	}
	for _, k := range agent.Harnesses(pr) {
		h := string(agent.HarnessOf(k))
		mean := agentName(pr) + "'s models, run by " + agentName(h) + "."
		if !agent.Runs(k) {
			mean = agentName(pr) + "'s models, run by " + agentName(h) + ", which isn't installed: it runs in the first that is."
		}
		choices = append(choices, [2]string{h, mean})
	}
	st := choiceSetting("Runs in", value, what, choices, set)
	st.unset, st.names = unset, map[string]string{}
	for _, c := range choices {
		if c[0] != "" {
			st.names[c[0]] = agentName(c[0])
		}
	}
	return st
}

// profileForm is one profile, open: its providers in order and where each
// runs (a provider's own: only where it runs), what it does when they run
// low, and the folders that pick it.
func (m *Model) profileForm(p state.Profile) []section {
	name := p.Name
	cfg := &m.store.Config
	change := func(f func(*state.Profile)) { m.changeProfile(name, f) }
	var first []section

	if ownProfile(name) {
		pr := agent.ProviderOf(agent.Kind(p.Providers[0]))
		if len(agent.Harnesses(pr)) > 1 {
			h := cfg.RunsIn[pr]
			if h == "" {
				h = string(agent.HarnessOf(agent.Harnesses(pr)[0]))
			}
			first = append(first, section{title: "Harness", note: "for every profile that doesn't choose its own",
				rows: []setting{harnessSetting(pr, h, "",
					"The program "+agentName(pr)+"'s sessions run in. Profiles of yours that list "+agentName(pr)+" use this, unless they choose another.",
					func(v string) {
						cfg.SetRunsIn(pr, v)
						_ = m.store.SaveConfig()
					})}})
		}
	} else {
		first = append(first, m.profileProviders(p, change))
	}

	limit := choiceSetting("When a limit stops a session", p.Limit(),
		"What a running conversation does when a usage limit stops it.",
		[][2]string{
			{state.LimitAccount, "agtop moves its provider to another account with room, and the conversation carries on."},
			{state.LimitHandoff, "another account first; when none has room, the conversation is handed to the next provider in the list, with what it was doing."},
			{state.LimitWait, "it waits for the limit to reset."},
		}, func(v string) { change(func(p *state.Profile) { p.OnLimit = v }) })
	limit.names = map[string]string{state.LimitAccount: "switch account", state.LimitHandoff: "hand off", state.LimitWait: "wait"}
	low := []setting{limit}
	if len(p.Providers) > 1 {
		mix := choiceSetting("When the first is out", firstNonEmpty(p.Mix, state.MixStay),
			"What new sessions do once every account of the first provider is nearly out. Running sessions stay where they are.",
			[][2]string{
				{state.MixStay, "new sessions still start on the first provider, and wait for its limits to reset."},
				{state.MixMix, "new sessions start on the next provider in the list with room, until the first's accounts reset."},
			}, func(v string) { change(func(p *state.Profile) { p.Mix = v }) })
		mix.names = map[string]string{state.MixStay: "wait for it", state.MixMix: "move on"}
		low = append([]setting{mix}, low...)
	}

	isDef := strings.EqualFold(cfg.Default().Name, name)
	def := choiceSetting("Default", map[bool]string{true: "yes", false: "no"}[isDef],
		"Whether a new session gets this profile when you haven't picked one and its folder has none.",
		[][2]string{{"yes", "it's the default."}, {"no", "sessions get it when you pick it (#profile " + name + ") or its folder does."}},
		func(v string) {
			if v == "yes" {
				cfg.SetDefaultProfile(name)
				m.spillTo()
			}
		})
	this := []setting{def}
	if !ownProfile(name) {
		rename := setting{label: "Name", value: name, typed: true, what: "What it's called, for #profile and the top bar.",
			run: func(string) tea.Cmd { return nil }}
		rename.key = func(s string) (tea.Cmd, bool) {
			if s == "enter" || s == "right" {
				m.renameProfile(name)
				return nil, true
			}
			return nil, false
		}
		this = append([]setting{rename}, this...)
	}
	return append(first,
		section{title: "When accounts run low", rows: low},
		section{title: "This profile", rows: this},
		m.folderSection(name),
	)
}

// profileProviders is a profile of yours' providers, in its order, then the
// installed ones it leaves out: enter takes one in or out, J and K move
// it, and h changes the harness it runs in under this profile.
func (m *Model) profileProviders(p state.Profile, change func(func(*state.Profile))) section {
	sec := section{title: "Providers", note: "new sessions start on the first; the rest are where they can move on to"}
	var provs []string
	for _, pr := range p.Providers {
		if agent.ProviderInstalled(agent.ProviderOf(agent.Kind(pr))) {
			provs = append(provs, pr)
		}
	}
	for _, pr := range installedProviders() {
		if !slices.Contains(provs, pr) {
			provs = append(provs, pr)
		}
	}
	used := 0
	for _, pr := range provs {
		in := slices.Contains(p.Providers, pr)
		sec.rows = append(sec.rows, m.providerRow(p, pr, in, used, change))
		if in {
			used++
		}
	}
	return sec
}

// providerRow is provider pr in profile p: number at+1 of it when in.
func (m *Model) providerRow(p state.Profile, pr string, in bool, at int, change func(func(*state.Profile))) setting {
	k := agent.Kind(p.KindOf(pr))
	many := len(agent.Harnesses(pr)) > 1
	keys := []string{"enter", "use or not", "J/K", "move"}
	if many && in {
		keys = append(keys, "h", "runs in")
	}
	return setting{
		label: agentName(pr),
		line: func(int) string {
			if !in {
				return faint("  ○ ") + glyph(agent.Kind(pr)) + " " + dim(fit(agentName(pr), 18)) + faint("not used")
			}
			n := paint(cOrange, strconv.Itoa(at+1))
			if !agent.Runs(k) {
				return n + faint(" ● ") + glyph(agent.Kind(pr)) + " " + paint(cText, fit(agentName(pr), 18)) + paint(cYellow, "agtop can't start its sessions yet")
			}
			where := ""
			switch {
			case many && p.RunsIn[pr] != "":
				where = dim(runsInWords(k))
			case many:
				where = faint(runsInWords(k))
			}
			return n + paint(cOrange, " ● ") + glyph(agent.Kind(pr)) + " " + paint(cText, fit(agentName(pr), 18)) + where
		},
		key: func(s string) (tea.Cmd, bool) {
			switch s {
			case "enter", "space":
				change(func(p *state.Profile) {
					switch {
					case !slices.Contains(p.Providers, pr):
						p.Providers = append(p.Providers, pr)
					case len(p.Providers) > 1:
						p.Providers = slices.DeleteFunc(p.Providers, func(q string) bool { return q == pr })
						delete(p.RunsIn, pr)
					default:
						m.flash("a profile needs one provider", true)
					}
				})
			case "K", "shift+up", "J", "shift+down":
				d := map[string]int{"K": -1, "shift+up": -1, "J": 1, "shift+down": 1}[s]
				change(func(p *state.Profile) {
					i := slices.Index(p.Providers, pr)
					if j := i + d; i >= 0 && j >= 0 && j < len(p.Providers) {
						p.Providers[i], p.Providers[j] = p.Providers[j], p.Providers[i]
						m.dialog.cursor += d
					}
				})
			case "h":
				if !many || !in {
					return nil, false
				}
				m.nextHarness(p, pr, change)
			default:
				return nil, false
			}
			return nil, true
		},
		keys: keys,
		about: func() (string, string, string) {
			what := "The providers this profile's sessions run, in order. New sessions start on the first; the others are where they go when its accounts are out, if the profile moves on or hands off."
			if many {
				what += " " + agentName(pr) + " can run in more than one harness: h chooses which, for this profile."
			}
			if !in {
				return agentName(pr), what, "Not used: enter adds it at the end."
			}
			return agentName(pr), what, fmt.Sprintf("Number %d, running as %s. enter drops it; J and K move it.", at+1, agentName(string(k)))
		},
	}
}

// nextHarness moves provider pr in profile p on to its next harness: its
// usual one, then each other, then back to whatever its own profile says.
func (m *Model) nextHarness(p state.Profile, pr string, change func(func(*state.Profile))) {
	hs := agent.Harnesses(pr)
	opts := make([]string, 1, len(hs)+1)
	for _, hk := range hs {
		opts = append(opts, string(agent.HarnessOf(hk)))
	}
	next := opts[(slices.Index(opts, p.RunsIn[pr])+1)%len(opts)]
	change(func(p *state.Profile) {
		if next == "" {
			delete(p.RunsIn, pr)
			return
		}
		if p.RunsIn == nil {
			p.RunsIn = map[string]string{}
		}
		p.RunsIn[pr] = next
	})
	q, _ := m.store.Config.ProfileNamed(p.Name)
	where := agentName(q.KindOf(pr))
	if next == "" {
		where += ", as " + agentName(pr) + "'s own profile says"
	}
	m.flash(agentName(pr)+" runs as "+where+" under "+p.Name, false)
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
