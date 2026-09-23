package ui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/actions"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/host"
)

// tempShown is the least temp work worth pointing out.
const tempShown = 1 << 20

// tempMsg brings temp-work sizes measured in the background.
type tempMsg map[string]fleet.TempSize

// measureTemp walks the temp folders of the agents whose temp work may have
// changed, in the background and one batch at a time: never-measured ones
// once, finished ones again only after they've done something, running ones
// every minute.
func (m *Model) measureTemp() tea.Cmd {
	if m.measuring || m.tick%5 != 1 {
		return nil
	}
	due := m.loader.Temp.Due(m.snap.Agents, time.Now())
	if len(due) == 0 {
		return nil
	}
	m.measuring = true
	keys := make([]string, len(due))
	dirs := make([][]fleet.TempDir, len(due))
	for i, a := range due {
		keys[i], dirs[i] = a.Key, a.TempDirs()
	}
	return func() tea.Msg {
		out := make(tempMsg, len(keys))
		for i, k := range keys {
			at := time.Now() // before the walk: anything written during it is measured next time
			out[k] = fleet.TempSize{Bytes: fleet.DiskUsage(dirs[i]), At: at}
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

// tempCell is a finished agent's temp work, for the row's resource columns
// that it no longer uses: nothing below a megabyte, loud from a gigabyte.
func tempCell(a *fleet.Agent, w int) string {
	if a.Temp < tempShown || w < 7 {
		return blanks(w)
	}
	v := right1("tmp "+disk(a.Temp), w)
	if a.Temp >= 1<<30 {
		return paint(cYellow, v)
	}
	return dim(v)
}

// askClean asks before deleting an agent's temp work.
func (m *Model) askClean(a *fleet.Agent) {
	if a == nil {
		m.flash("select an agent first", true)
		return
	}
	if a.PID != 0 {
		m.flash(a.DisplayName+" is still running; its temp work may be in use · stop it first (ctrl+x)", true)
		return
	}
	if a.Temp < tempShown {
		m.flash(a.DisplayName+" has no temp work to clean", false)
		return
	}
	m.confirm = &confirmation{
		question: fmt.Sprintf("Delete %s of %s's temp work?", disk(a.Temp), a.DisplayName),
		detail:   "its scratch folders; the conversation, its files and any worktree stay",
		onYes:    func() tea.Cmd { return m.cleanTemp([]*fleet.Agent{a}) },
	}
}

// askCleanAll asks before deleting the temp work of every agent that has
// finished.
func (m *Model) askCleanAll() {
	var list []*fleet.Agent
	var total int64
	for _, a := range m.snap.Agents {
		if a.PID == 0 && a.Temp >= tempShown {
			list = append(list, a)
			total += a.Temp
		}
	}
	if len(list) == 0 {
		m.flash("no finished agent has temp work to clean", false)
		return
	}
	m.confirm = &confirmation{
		question: fmt.Sprintf("Delete %s of temp work from %d finished agents?", disk(total), len(list)),
		detail:   "their scratch folders; conversations, files and worktrees stay · running agents are left alone",
		onYes:    func() tea.Cmd { return m.cleanTemp(list) },
	}
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
// so an idle process of its goes too (a message resumes it); one still
// working is only stopped if you say so, and temp work can go with it.
func (m *Model) markDone(a *fleet.Agent) tea.Cmd {
	if a == nil {
		m.flash("select an agent first", true)
		return nil
	}
	if a.Done {
		m.toggleDone(a)
		return nil
	}
	stop := func() tea.Cmd {
		switch {
		case a.PID == 0 || a.Interactive:
			return nil // nothing resident, or it's open in a terminal: leave it be
		case a.Agtop:
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
		return cmdErr("", func() error { return actions.Stop(a.Acct, a.ID, a.PID) })
	}
	done := func(clean bool) tea.Cmd {
		m.toggleDone(a)
		cmd := stop()
		if a.PID != 0 && !a.Interactive {
			m.flash("done: "+a.DisplayName+" · its process stopped, a message resumes it", false)
		}
		if !clean {
			return cmd
		}
		b := *a
		b.PID = 0 // stopping; its temp work goes once it has
		return tea.Sequence(cmd, func() tea.Msg { time.Sleep(time.Second); return nil }, m.cleanTemp([]*fleet.Agent{&b}))
	}
	busy := a.Live() || a.Busy()
	if !busy && a.Temp < tempShown {
		return done(false)
	}
	c := &confirmation{question: "Mark " + a.DisplayName + " done?", onYes: func() tea.Cmd { return done(false) }}
	switch {
	case busy && a.Interactive:
		c.detail = "it's still working, in its terminal; it keeps going there"
	case busy:
		c.detail = "it's still working · y stops it"
	case a.PID != 0 && !a.Interactive:
		c.detail = "stops its idle process; a message resumes it"
	}
	if a.Temp >= tempShown && !a.Interactive {
		c.bangText = "also delete its " + disk(a.Temp) + " of temp work"
		c.onBang = func() tea.Cmd { return done(true) }
		if c.detail == "" {
			c.detail = disk(a.Temp) + " of temp work stays unless you choose !"
		}
	}
	m.confirm = c
	return nil
}
