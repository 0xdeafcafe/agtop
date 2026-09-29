package ui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/host"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// Profiles decide which provider a session runs and what it does at a
// usage limit (state.Profile). A session's is the one picked for it (the
// picker or #profile), else its folder's rule, else the default; the host
// keeps its name, so a resumed session keeps it too.

// room is what agtop knows of each installed provider's accounts: which
// is in use, and which are nearly out.
func (m *Model) room() state.Room {
	room := state.Room{}
	heads := map[agent.Kind]acctRow{}
	for _, r := range m.accountRows() {
		k := string(r.kind)
		if r.head {
			heads[r.kind] = r
			continue
		}
		room[k] = append(room[k], state.Seat{ID: r.id(), Name: r.name(), Current: r.current, Out: nearlyOut(r.q), Used: r.q.Used("")})
	}
	// A provider with no accounts of its own kept: its one sign-in.
	for k, r := range heads {
		if len(room[string(k)]) == 0 && len(r.q.Windows) > 0 {
			room[string(k)] = []state.Seat{{Name: r.name(), Current: true, Out: nearlyOut(r.q), Used: r.q.Used("")}}
		}
	}
	return room
}

// id is the account's own id: a login's uuid, or another agent's.
func (r acctRow) id() string {
	if r.login != nil {
		return r.login.ID
	}
	return r.acct.ID
}

// startProfile is the profile a session started in dir gets.
func (m *Model) startProfile(dir string) state.Profile {
	return m.store.Config.ProfileFor(dir, m.accts.profile)
}

// startPick is the provider and account a session started in dir runs
// on, as its profile picks.
func (m *Model) startPick(dir string) (state.Pick, bool) {
	return m.startProfile(dir).Pick(m.room())
}

// sessionProfile is the profile a session runs under: the one it was
// started with, else what its folder would get now.
func (m *Model) sessionProfile(a *fleet.Agent) state.Profile {
	return m.store.Config.ProfileFor(a.Cwd, a.Profile)
}

// limitStopped is whether an agtop session of provider k is stopped by a
// usage limit, under a profile that moves on from it.
func (m *Model) limitStopped(k agent.Kind) bool {
	for _, a := range m.snap.Agents {
		if a.Agtop && agent.Kind(a.Kind) == k && strings.HasPrefix(a.Detail, "usage limit") && m.sessionProfile(a).Limit() != state.LimitWait {
			return true
		}
	}
	return false
}

// usePickedProfile is #profile: the profile the next session from the
// Prompt starts under, or, with none named, which it is and what else
// there is.
func (m *Model) usePickedProfile(name string) {
	cfg := m.store.Config
	name = strings.TrimSpace(name)
	if name == "" {
		var names []string
		for _, p := range cfg.Profiles {
			names = append(names, p.Name)
		}
		m.flash("the next session runs "+m.startProfile(m.startDir()).Name+" · profiles: "+strings.Join(names, ", "), false)
		return
	}
	p, ok := cfg.ProfileNamed(name)
	if !ok {
		m.flash("no profile named "+name, true)
		return
	}
	m.accts.profile = p.Name
	m.flash("the next session starts under "+p.Name+" · "+m.profileWords(p), false)
}

// profileWords says what a profile does, in a line.
func (m *Model) profileWords(p state.Profile) string {
	var names []string
	for _, k := range p.Installed() {
		names = append(names, agentName(k))
	}
	if len(names) == 0 {
		return "none of its providers is installed"
	}
	s := strings.Join(names, " → ")
	if !p.Mixes() {
		s = names[0] + " only"
	}
	return s + " · at a limit: " + limitWords(p.Limit())
}

// limitWords is a profile's OnLimit in words.
func limitWords(v string) string {
	switch v {
	case state.LimitWait:
		return "wait for the reset"
	case state.LimitHandoff:
		return "another account, then hand on"
	}
	return "another account"
}

// handOffStopped hands each conversation a usage limit stopped, under a
// profile that hands on, to the next provider with room, once none of
// its own provider's accounts has any.
func (m *Model) handOffStopped() tea.Cmd {
	m.accts.ready()
	room := m.room()
	var cmds []tea.Cmd
	for _, a := range m.snap.Agents {
		if !a.Agtop || !strings.HasPrefix(a.Detail, "usage limit") || m.accts.handedOff[a.Key] {
			continue
		}
		p := m.sessionProfile(a)
		if p.Limit() != state.LimitHandoff {
			continue
		}
		kind := a.Kind
		if _, ok := p.PickFor(kind, room); ok {
			continue // another of its accounts has room: switching comes first
		}
		// Only to a provider that can start from another's conversation.
		takers := p
		takers.Providers = slices.DeleteFunc(slices.Clone(p.Providers), func(k string) bool {
			return k != kind && !agent.Supports(agent.Kind(k), agent.FeatureHandoffIn)
		})
		to, ok := takers.Next(kind, room)
		if !ok {
			continue
		}
		m.accts.handedOff[a.Key] = true
		cmds = append(cmds, m.handOff(a, to, p))
	}
	return tea.Batch(cmds...)
}

// handOff starts a new session on another provider carrying a
// conversation on: in the same folder, under the same profile, opened
// with the conversation so far. The one a limit stopped is left as it is.
func (m *Model) handOff(a *fleet.Agent, to state.Pick, p state.Profile) tea.Cmd {
	in := agent.Handoff(m.conversationOf(a))
	cfg := host.Config{Cwd: a.Cwd, Prompt: in.Text, Images: in.Images, Name: a.DisplayName + " · on " + agentName(to.Kind), Profile: p.Name,
		IdleStop: host.Duration(m.store.Config.Dispatch.Rest())}
	if err := cfg.UseAgent(to.Kind); err != nil {
		m.flash("couldn't hand "+a.DisplayName+" on: "+err.Error(), true)
		return nil
	}
	m.flash(a.DisplayName+" is out of "+agentName(a.Kind)+" · handing it to "+agentName(to.Kind), false)
	return func() tea.Msg {
		c, err := host.Spawn(cfg)
		if err != nil {
			return doneMsg{err: err}
		}
		return hostStartedMsg{id: c.ID, name: cfg.Name}
	}
}

// openProfilePicker is alt+w: a picker of the profiles, to make one the
// default, then the installed providers, to put one first in the default
// profile, then Profiles itself.
func (m *Model) openProfilePicker() {
	cfg := m.store.Config
	def := cfg.Default()
	p := &picker{title: "New sessions run"}
	for _, pr := range cfg.Profiles {
		name := pr.Name
		mark := "  "
		if strings.EqualFold(name, def.Name) {
			mark = paint(cOrange, "★ ")
			p.cursor = len(p.acts)
		}
		p.acts = append(p.acts, linkAct{
			label: mark + paint(cText+bold, fit(name, 14)) + " " + m.chain(pr),
			do: func(m *Model) tea.Cmd {
				m.store.Config.SetDefaultProfile(name)
				_ = m.store.SaveConfig()
				m.spillTo()
				m.flash(name+" is the default profile · "+m.profileWords(m.store.Config.Default()), false)
				return nil
			},
		})
	}
	for _, ad := range m.agentOrder() {
		k := ad.Kind()
		if !agent.Runs(k) {
			continue
		}
		note := faint("  put first in " + def.Name)
		if inst := def.Installed(); len(inst) > 0 && inst[0] == string(k) {
			note = dim("  first in " + def.Name)
		}
		p.acts = append(p.acts, linkAct{
			label: "  " + providerTag(k) + note,
			do: func(m *Model) tea.Cmd {
				m.withAgent(string(k))
				m.spillTo()
				return nil
			},
		})
	}
	p.acts = append(p.acts, linkAct{
		label: faint("  edit profiles…"),
		do: func(m *Model) tea.Cmd {
			m.setView(placeSettings)
			m.setSettingsPage(pageProfiles)
			return nil
		},
	})
	m.picker = p
}
