// Package ui is the agents view: the native layout plus cost, time, CPU/RAM,
// preview, processes, accounts, groups and folder moves.
package ui

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agents/internal/actions"
	"github.com/0xdeafcafe/agents/internal/claude"
	"github.com/0xdeafcafe/agents/internal/daemon"
	"github.com/0xdeafcafe/agents/internal/fleet"
	"github.com/0xdeafcafe/agents/internal/state"
)

type mode int

const (
	modeList mode = iota
	modeProcs
	modeAccounts
	modeCwd
	modeHelp
)

type inputKind int

const (
	inPrompt inputKind = iota
	inRename
	inGroup
	inNewAccount
)

var groupModes = []string{"status", "repo", "account", "group"}

type confirmation struct {
	question string
	detail   string
	onYes    func() tea.Cmd
	onBang   func() tea.Cmd
	bangText string
}

type Model struct {
	store     *state.Store
	loader    *fleet.Loader
	scanner   *fleet.Scanner
	snap      *fleet.Snapshot
	launchDir string
	version   string

	w, h     int
	mode     mode
	tick     int
	scanning bool
	loaded   bool

	sel      string
	order    []*fleet.Agent
	lines    []listLine
	scroll   int
	preview  bool
	previews map[string]previewEntry
	expanded map[string]bool

	input  []rune
	inKind inputKind

	status    string
	statusErr bool
	statusAt  time.Time
	armed     string
	quitArmed time.Time
	confirm   *confirmation

	procCursor  int
	procMachine bool
	acctCursor  int
	cwdMove     bool
	cwdCursor   int
	cwdFor      string

	lastState map[string]string
}

type previewEntry struct {
	p    claude.Preview
	size int64
}

type listLine struct {
	header string
	agent  *fleet.Agent
	extra  string
}

func New(store *state.Store, version string) *Model {
	dir, _ := os.Getwd()
	m := &Model{
		store: store, loader: fleet.NewLoader(store), scanner: fleet.NewScanner(),
		launchDir: dir, version: version, previews: map[string]previewEntry{},
		expanded: map[string]bool{}, lastState: map[string]string{}, cwdMove: true,
	}
	if store.Config.GroupBy == "" {
		store.Config.GroupBy = "status"
	}
	m.snap = m.loader.Load(true)
	m.rebuild()
	return m
}

type tickMsg time.Time
type scanMsg map[string]fleet.Spend
type previewMsg struct {
	key string
	e   previewEntry
}
type doneMsg struct {
	text string
	err  error
}
type movedMsg struct {
	from *fleet.Agent
	to   string
}
type attachDoneMsg struct {
	agent *fleet.Agent
	err   error
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) Init() tea.Cmd { return tea.Batch(tick(), m.scan()) }

func (m *Model) scan() tea.Cmd {
	if m.scanning {
		return nil
	}
	m.scanning = true
	targets := m.targets()
	sc := m.scanner
	return func() tea.Msg { return scanMsg(sc.Run(targets)) }
}

func (m *Model) targets() []fleet.Target {
	var targets []fleet.Target
	for _, a := range m.snap.Agents {
		if a.TranscriptPath != "" {
			targets = append(targets, fleet.Target{Key: a.Key, Path: a.TranscriptPath})
		}
	}
	return targets
}

func (m *Model) loadPreview() tea.Cmd {
	a := m.selected()
	if a == nil || a.TranscriptPath == "" {
		return nil
	}
	st, err := os.Stat(a.TranscriptPath)
	if err != nil {
		return nil
	}
	if e, ok := m.previews[a.Key]; ok && e.size == st.Size() {
		return nil
	}
	key, path, size := a.Key, a.TranscriptPath, st.Size()
	return func() tea.Msg {
		return previewMsg{key: key, e: previewEntry{p: claude.ReadPreview(path, 96<<10), size: size}}
	}
}

func (m *Model) flash(s string, err bool) {
	m.status, m.statusErr, m.statusAt = s, err, time.Now()
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil
	case tickMsg:
		m.tick++
		m.refresh()
		cmds := []tea.Cmd{tick()}
		if m.tick%3 == 0 {
			cmds = append(cmds, m.scan())
		}
		if m.preview || m.mode == modeProcs {
			cmds = append(cmds, m.loadPreview())
		}
		return m, tea.Batch(cmds...)
	case scanMsg:
		m.scanning = false
		m.loader.SetSpend(msg)
		m.loaded = true
		m.refresh()
		return m, nil
	case previewMsg:
		m.previews[msg.key] = msg.e
		return m, nil
	case doneMsg:
		if msg.err != nil {
			m.flash(msg.err.Error(), true)
		} else if msg.text != "" {
			m.flash(msg.text, false)
		}
		m.refresh()
		return m, nil
	case movedMsg:
		// The old row keeps its history; it moves to Done and points at the new one.
		m.store.Overlay.Moved[msg.from.Key] = msg.to
		m.store.Overlay.Done[msg.from.Key] = time.Now()
		if n := m.store.Overlay.Names[msg.from.Key]; n != "" {
			m.store.Overlay.Names[msg.to] = n
		}
		_ = m.store.SaveOverlay()
		m.sel = msg.to
		m.flash("moved — now "+strings.TrimPrefix(msg.to, msg.from.Acct.Name+"/"), false)
		m.refresh()
		return m, nil
	case attachDoneMsg:
		m.refresh()
		if msg.err != nil && msg.agent != nil {
			if daemon.IsRefusal(msg.err, "EKICKED") {
				m.flash("opened in another window", false)
				return m, nil
			}
			a := msg.agent
			return m, tea.ExecProcess(actions.AttachFallback(a.Acct, a.ID), func(err error) tea.Msg {
				return doneMsg{err: err}
			})
		}
		return m, nil
	case tea.PasteMsg:
		if m.acceptsText() {
			m.input = append(m.input, []rune(oneLine(msg.Content))...)
		}
		return m, nil
	case tea.KeyPressMsg:
		return m, m.key(msg)
	}
	return m, nil
}

func (m *Model) acceptsText() bool {
	return m.confirm == nil && (m.mode == modeList || m.mode == modeCwd || m.inKind == inNewAccount)
}

func (m *Model) refresh() {
	m.snap = m.loader.Load(true)
	m.notify()
	m.hibernate()
	m.rebuild()
}

// notify posts a notification when an agent starts waiting on the user.
func (m *Model) notify() {
	first := len(m.lastState) == 0
	for _, a := range m.snap.Agents {
		prev := m.lastState[a.Key]
		m.lastState[a.Key] = a.State
		if !first && prev != "" && prev != "blocked" && a.State == "blocked" {
			body := a.Needs
			if body == "" {
				body = oneLine(a.Detail)
			}
			actions.Notify(a.DisplayName+" needs you", body)
		}
	}
}

// hibernate stops finished agents whose process is still resident.
func (m *Model) hibernate() {
	after := m.store.Config.Hibernate.AfterMinutes
	if after <= 0 {
		return
	}
	for _, a := range m.snap.Agents {
		if a.Worker != nil && a.State == "done" && a.Age(m.snap.At) > time.Duration(after)*time.Minute {
			go actions.Stop(a.Acct, a.ID)
		}
	}
}

func (m *Model) selected() *fleet.Agent {
	for _, a := range m.order {
		if a.Key == m.sel {
			return a
		}
	}
	return nil
}

func (m *Model) selIndex() int {
	for i, a := range m.order {
		if a.Key == m.sel {
			return i
		}
	}
	return -1
}

func (m *Model) move(d int) {
	if len(m.order) == 0 {
		return
	}
	i := m.selIndex() + d
	if i < 0 {
		i = 0
	}
	if i >= len(m.order) {
		i = len(m.order) - 1
	}
	m.sel = m.order[i].Key
	m.armed = ""
}

// rebuild groups the agents for the current group-by mode.
func (m *Model) rebuild() {
	by := m.store.Config.GroupBy
	type group struct {
		name   string
		agents []*fleet.Agent
		rank   int
		recent time.Time
	}
	groups := map[string]*group{}
	add := func(name string, rank int, a *fleet.Agent) {
		g := groups[name]
		if g == nil {
			g = &group{name: name, rank: rank}
			groups[name] = g
		}
		g.agents = append(g.agents, a)
		if a.UpdatedAt.After(g.recent) {
			g.recent = a.UpdatedAt
		}
	}
	statusGroup := func(a *fleet.Agent) {
		switch {
		case a.State == "blocked":
			add("Awaiting input", 1, a)
		case a.Live():
			add("Working", 2, a)
		default:
			add("Completed", 8, a)
		}
	}
	for _, a := range m.snap.Agents {
		switch {
		case a.Pinned:
			add("Pinned", 0, a)
		case a.Done:
			add("Done", 9, a)
		case by == "repo":
			name := "No repository"
			if a.Repo != "" {
				name = tildify(a.Repo)
				if a.Branch != "" {
					name += " · " + a.Branch
				}
			}
			add(name, 3, a)
		case by == "account":
			add(a.Acct.Name, 3, a)
		case by == "group" && a.Group != "":
			add(a.Group, 3, a)
		default:
			statusGroup(a)
		}
	}
	list := make([]*group, 0, len(groups))
	for _, g := range groups {
		list = append(list, g)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].rank != list[j].rank {
			return list[i].rank < list[j].rank
		}
		return list[i].recent.After(list[j].recent)
	})
	m.order = m.order[:0]
	m.lines = m.lines[:0]
	for _, g := range list {
		header := g.name
		if by != "status" && g.rank == 3 {
			var cost float64
			for _, a := range g.agents {
				cost += a.Spend.Cost
			}
			header = fmt.Sprintf("%s\x00%d agents · %s", g.name, len(g.agents), money(cost))
		}
		m.lines = append(m.lines, listLine{header: header})
		for _, a := range g.agents {
			m.order = append(m.order, a)
			m.lines = append(m.lines, listLine{agent: a})
			if m.expanded[a.Key] {
				for _, l := range m.expandLines(a) {
					m.lines = append(m.lines, listLine{extra: l})
				}
			}
		}
		m.lines = append(m.lines, listLine{})
	}
	if m.selected() == nil && len(m.order) > 0 {
		m.sel = m.order[0].Key
	}
}

func (m *Model) expandLines(a *fleet.Agent) []string {
	var out []string
	text := a.Detail
	if a.State == "blocked" && a.Needs != "" {
		text = "needs: " + a.Needs
	}
	for _, l := range wrap(text, max(20, m.w-10)) {
		out = append(out, l)
	}
	if a.Cwd != "" {
		out = append(out, tildify(a.Cwd))
	}
	return out
}

func tildify(p string) string {
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

func (m *Model) account(name string) claude.Account {
	for _, a := range m.store.Config.AllAccounts() {
		if a.Name == name {
			return a
		}
	}
	return m.store.Config.ActiveAccount()
}

func cmdErr(text string, f func() error) tea.Cmd {
	return func() tea.Msg {
		if err := f(); err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{text: text}
	}
}

func (m *Model) attach(a *fleet.Agent) tea.Cmd {
	if a == nil {
		return nil
	}
	s := &daemon.Session{Client: daemon.Client{Account: a.Acct}, Short: a.ID}
	return tea.Exec(s, func(err error) tea.Msg { return attachDoneMsg{agent: a, err: err} })
}

func (m *Model) togglePin(a *fleet.Agent) tea.Cmd {
	pins := claude.ReadPins(a.Acct)
	out := pins[:0:0]
	found := false
	for _, id := range pins {
		if id == a.ID {
			found = true
			continue
		}
		out = append(out, id)
	}
	if !found {
		out = append(out, a.ID)
	}
	return cmdErr("", func() error { return claude.WritePins(a.Acct, out) })
}

func (m *Model) toggleDone(a *fleet.Agent) {
	if _, ok := m.store.Overlay.Done[a.Key]; ok {
		delete(m.store.Overlay.Done, a.Key)
		m.flash("moved back: "+a.DisplayName, false)
	} else {
		m.store.Overlay.Done[a.Key] = time.Now()
		m.flash("done: "+a.DisplayName, false)
	}
	_ = m.store.SaveOverlay()
	m.refresh()
}

func (m *Model) nativeView() tea.Cmd {
	c := exec.Command("claude", "agents")
	c.Env = m.store.Config.ActiveAccount().Env()
	return tea.ExecProcess(c, func(err error) tea.Msg { return doneMsg{err: err} })
}
