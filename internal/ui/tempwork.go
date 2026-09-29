package ui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/fleet"
	"github.com/0xdeafcafe/rush/internal/host"
)

// tempShown is the least temp work worth pointing out.
const tempShown = 1 << 20

// tempLoud is temp work big enough to point out in yellow.
const tempLoud = 1 << 30

// tempMsg brings temp-work sizes measured in the background.
type tempMsg map[string]fleet.TempSize

// measureTemp walks the temp folders of the agents whose temp work may have
// changed, in the background and one batch at a time: never-measured ones
// once, finished ones again only after they've done something, running ones
// every minute (longer for ones slow to walk).
func (m *Model) measureTemp() tea.Cmd {
	if m.measuring || m.tick%5 != 1 {
		return nil
	}
	due := m.loader.Temp.Due(m.snap.Agents, time.Now())
	if len(due) == 0 {
		return nil
	}
	m.measuring = true
	// Copies, for the command: where their temp work is is worked out
	// there too (it asks the system for the user's id).
	agents := make([]fleet.Agent, len(due))
	for i, a := range due {
		agents[i] = *a
	}
	return func() tea.Msg {
		out := make(tempMsg, len(agents))
		for i := range agents {
			at := time.Now() // before the walk: anything written during it is measured next time
			n := fleet.DiskUsage(agents[i].TempDirs())
			out[agents[i].Key] = fleet.TempSize{Bytes: n, At: at, Took: time.Since(at)}
		}
		return out
	}
}

func (m *Model) onTemp(msg tempMsg) {
	m.measuring = false
	m.loader.Temp.Set(msg)
	for _, a := range m.snap.Agents {
		if e, ok := msg[a.Key]; ok {
			a.Temp = e.Bytes
		}
	}
	m.rebuild()
}

// tempTotal is all the agents' temp work.
func (m *Model) tempTotal() int64 {
	var n int64
	for _, a := range m.snap.Agents {
		n += a.Temp
	}
	return n
}

// disk formats bytes of disk the way mem formats memory.
func disk(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%dM", n>>20)
	case n > 0:
		return fmt.Sprintf("%dK", n>>10)
	}
	return "–"
}

// askClean opens Delete on an agent's temp work.
func (m *Model) askClean(a *fleet.Agent) tea.Cmd {
	if a == nil {
		m.flash("select an agent first", true)
		return nil
	}
	if a.PID != 0 {
		m.flash(a.DisplayName+" is still running; its temp work may be in use · stop it first (ctrl+x)", true)
		return nil
	}
	if a.Temp < tempShown {
		m.flash(a.DisplayName+" has no temp work to clean", false)
		return nil
	}
	return m.openDelete(nil, doomed{agents: []*fleet.Agent{a}})
}

// askCleanAll opens Delete on the temp work of every agent that has
// finished; running agents are left alone.
func (m *Model) askCleanAll() tea.Cmd {
	var list []*fleet.Agent
	for _, a := range m.snap.Agents {
		if a.PID == 0 && a.Temp >= tempShown {
			list = append(list, a)
		}
	}
	if len(list) == 0 {
		m.flash("no finished agent has temp work to clean", false)
		return nil
	}
	return m.openDelete(nil, doomed{agents: list})
}

// cleanTemp deletes agents' temp work in the background, then measures it
// again.
func (m *Model) cleanTemp(list []*fleet.Agent) tea.Cmd {
	var total int64
	for _, a := range list {
		total += a.Temp
	}
	m.flash("cleaning "+disk(total)+" of temp work…", false)
	return func() tea.Msg {
		sizes := tempMsg{}
		var failed error
		for _, a := range list {
			if err := fleet.CleanTemp(a); err != nil && failed == nil {
				failed = err
			}
			sizes[a.Key] = fleet.TempSize{Bytes: fleet.DiskUsage(a.TempDirs()), At: time.Now()}
		}
		var freed int64
		for _, a := range list {
			freed += a.Temp - sizes[a.Key].Bytes
		}
		return cleanedMsg{sizes: sizes, freed: freed, err: failed}
	}
}

type cleanedMsg struct {
	sizes tempMsg
	freed int64
	err   error
}

func (m *Model) onCleaned(msg cleanedMsg) {
	m.loader.Temp.Set(msg.sizes)
	for _, a := range m.snap.Agents {
		if e, ok := msg.sizes[a.Key]; ok {
			a.Temp = e.Bytes
		}
	}
	m.rebuild()
	if msg.err != nil {
		m.flash("freed "+disk(msg.freed)+" · "+msg.err.Error(), true)
		return
	}
	m.flash("freed "+disk(msg.freed)+" of temp work", false)
}

// markDone moves an agent to Done, or back. Done work shouldn't hold memory,
// so an idle process of its goes too (a message resumes it); only one still
// working asks first. Temp work stays; /clean is for that.
func (m *Model) markDone(a *fleet.Agent) tea.Cmd {
	if a == nil {
		m.flash("select an agent first", true)
		return nil
	}
	m.didStep("done")
	if a.Done {
		m.toggleDone(a)
		return nil
	}
	done := func() tea.Cmd {
		m.toggleDone(a)
		switch {
		case a.PID == 0 || a.Interactive:
			return nil // nothing resident, or it's open in a terminal: leave it be
		case a.Rush:
			m.flash("done: "+a.DisplayName+" · its process stopped, a message resumes it", false)
			id := a.ID
			return cmdErr("", func() error {
				c, err := host.Dial(id)
				if err != nil {
					return nil // already gone
				}
				defer c.Close()
				return c.Stop()
			})
		}
		m.flash("done: "+a.DisplayName+" · its process stopped, a message resumes it", false)
		return cmdErr("", func() error { return stopOutside(a) })
	}
	if !a.Live() && !a.Busy() {
		return done()
	}
	c := &confirmation{question: "Mark " + a.DisplayName + " done?", onYes: done}
	if a.Interactive {
		c.detail = "it's still working, in its terminal; it keeps going there"
	} else {
		c.detail = "it's still working · y stops it"
	}
	m.confirm = c
	return nil
}
