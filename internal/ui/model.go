// Package ui is the agtop view: the native layout plus cost, time, CPU/RAM,
// preview, processes, accounts, groups and folder moves.
package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/actions"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/daemon"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/host"
	"github.com/0xdeafcafe/agtop/internal/menubar"
	"github.com/0xdeafcafe/agtop/internal/state"
	"github.com/0xdeafcafe/agtop/internal/statusline"
	"github.com/0xdeafcafe/agtop/internal/update"
)

type mode int

const (
	modeList mode = iota
	modeProcs
	modeCleanup
	modeCwd
	modeHelp
	modeEff
)

type inputKind int

const (
	inPrompt inputKind = iota
	inRename
	inGroup
	inReply
)

var groupModes = []string{"status", "repo", "account", "group"}

type confirmation struct {
	question string
	detail   string
	onYes    func() tea.Cmd
	onBang   func() tea.Cmd
	bangText string
	// A question between two choices rather than yes or cancel: y and n
	// each do something, labelled yesText and noText; esc still cancels.
	yesText, noText string
	onNo            func() tea.Cmd
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

	// switching is set while ~/.claude is being signed in as another
	// login; switchedAt is when it last was.
	switching  bool
	switchedAt time.Time
	keepFailed bool      // said once until it works again
	resumedAt  time.Time // when sessions a limit stopped were last told to carry on

	sel          string
	shown        string // the agent last picked, still shown while a folded section is
	order        []*fleet.Agent
	lines        []listLine
	scroll       int
	preview      bool
	full         bool
	peekFrom     string // the agent whose Session alone you left to peek at Agents
	previews     map[string]previewEntry
	btws         map[string]*btwThread // agents' side threads (/btw), by key
	live         *live
	liveOpening  string
	liveFailed   string
	liveFailedAt time.Time
	hover        string
	rowKeys      []string
	listTop      int
	lastClick    time.Time

	input  []rune
	back   int // cursor distance from the input's end
	anchor int // selection start + 1; 0 when nothing is selected
	// pendingCopy is text to put on the clipboard with the next update.
	pendingCopy string
	// Where the Prompt's box was drawn, so a click can place the cursor.
	promptBox    box
	promptBoxIdx int
	promptBoxY   int
	inKind       inputKind
	slashSel     int // the Prompt's command picker's selection
	// paneFocus sends keys to an agtop-mode session's pane instead of the
	// list and its prompt.
	paneFocus bool
	dragging  bool     // resizing the list by its edge
	boxDrag   int      // selecting by dragging in an input box: 1 the Session's, 2 the prompt's
	images    []string // image files attached to the prompt's next message
	// claudeView is a Claude Code agent's Session view: 0 its live screen,
	// 1 the summary.
	claudeView int
	zen        bool // the Zen view: only the agent that needs you
	peek       zenPeek
	frameLen   int // bytes in the last frame, to size the next
	lastKeyAt  time.Time
	paneTop    int // screen row of the pane's first line, for clicks
	// host is the connection to the agtop-mode session the pane shows.
	host        *hostConn
	hostOpening string
	dirIdx      int

	status    string
	statusErr bool
	statusAt  time.Time
	armed     string
	quitArmed time.Time
	confirm   *confirmation
	dialog    *dialog
	picker    *picker
	sheet     sheet // /fork, /rewind, /plugins, /statusline, /skills: see sheet.go
	// rewound holds the message /rewind put back, by agent, for the box
	// once the pane reconnects.
	rewound   map[string]string
	bars      statusline.Bars  // agtop's own status lines: see bars.go
	barDrops  map[int][]string // segments each of their lines last left out for room
	embedded  bool
	promptFor string
	listW     int
	pastes    pastes // long pastes in the main box, shown as chips
	blurred   bool   // the terminal says agtop isn't the focused window
	sameFrame bool   // the last message changed nothing on screen
	lastFrame string // what View drew last
	// openFailed is when a Session last failed to open, by agent key; zen
	// skips those for a while rather than sticking on one it can't show.
	openFailed   map[string]time.Time
	localQ       map[string]*localQueue // messages waiting for Claude Code sessions, by agent key
	moveWhenIdle map[string]bool        // agents to move to agtop mode when their turn ends
	divHover     bool                   // the mouse is on the edge between Agents and the Session
	pointer      string                 // the pointer's shape last asked of the terminal
	sheetAt      [2]int                 // where the open sheet's body was drawn: x, y
	hibernated   map[string]bool
	offline      bool // never ask Anthropic for usage (--soak)
	// newer is the agtop that's out when it's newer than this one; #update
	// installs it.
	newer        update.Info
	updating     bool
	armedAt      time.Time
	attached     string
	view         int
	machinePage  int  // the Machine place's page: Processes or Cleanup
	settingsPage int  // the Settings place's page: a tab of the dialog
	helpPage     int  // the guide's tab: helpPages
	onboard      bool // teaching: Getting started and tips
	cardShown    bool // Getting started was under the list last frame

	procCursor int
	procPID    int // the process the cursor is on, followed as the list reorders
	cwdMove    bool
	cwdCursor  int
	cwdFor     string

	lastState map[string]string
	lastErr   map[string]bool // agents last seen with an error, so one starting is noticed
	fx        clkFX           // what clanker is reacting to
	fxOn      bool            // his reaction is ticking
	fxKick    bool            // a reaction started this update; its ticking needs starting
	clkMark   bool            // clanker is the monogram for now
	clkMarkAt int             // the tick he turned into it
	measuring bool            // temp work is being measured in the background
	clean     cleanup         // the Cleanup view's worktrees, and the tidy-up
	eff       effState        // the Efficiency place
	reaper    fleet.Reaper    // ends what agents leave running when they stop
	squeezing bool            // transcripts are being compressed in the background

	bar     *cmdBar  // the command bar, while it's open
	barBack *spot    // where the bar last jumped from
	jump    *barJump // a jump into a conversation that's still opening
	// groupOf is the list section each agent is in, folded or not.
	groupOf map[string]string
}

type previewEntry struct {
	p    claude.Preview
	size int64
}

type lineKind int

const (
	lineBlank lineKind = iota
	lineSection
	lineAgent
)

type listLine struct {
	kind   lineKind
	title  string
	meta   string
	folded bool
	peek   string
	agent  *fleet.Agent
}

func sectionKey(title string) string { return "§" + title }

func New(store *state.Store, version string) *Model {
	dir, _ := os.Getwd()
	m := &Model{
		store: store, loader: fleet.NewLoader(store), scanner: fleet.NewScanner(),
		launchDir: dir, version: version, previews: map[string]previewEntry{},
		lastState: map[string]string{}, cwdMove: true,
		hibernated: map[string]bool{},
		bars:       statusline.LoadBars(),
	}
	if store.Config.GroupBy == "" {
		store.Config.GroupBy = "status"
	}
	applyColors(store.Config.ColorBlind)
	convo.SetShowWhitespace(store.Config.ShowWhitespace)
	m.snap = m.loader.Load(true)
	m.rebuild()
	m.onboard = true
	return m
}

type tickMsg time.Time
type usageMsg struct {
	dir string
	u   claude.Usage
}
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

func (m *Model) Init() tea.Cmd {
	return tea.Batch(tick(), m.scan(), m.fetchUsage(), m.findLogins(), m.startMenuBar(), m.startView(), m.checkUpdate())
}

// startView opens on the layout you kept, or asks which, the first time.
// Agents alone kept before there was a choice counts as picking it.
func (m *Model) startView() tea.Cmd {
	c := &m.store.Config
	if c.View == "" && c.ListOnly {
		c.View = "list"
	}
	if c.View == "" {
		m.askView()
		return nil
	}
	cmd := m.openView()
	m.askMenuBar()
	return cmd
}

// startMenuBar opens the menu bar icon when it's on, building it first if
// agtop changed since; one left running by an older agtop is replaced.
func (m *Model) startMenuBar() tea.Cmd {
	if !m.store.Config.MenuBar || m.offline {
		return nil
	}
	return hostCmd(menubar.Start)
}

// fetchUsage refreshes every account's plan usage from Anthropic. Readings
// are shared with every other agtop through a file, so an account is asked
// only when its last reading is older than claude.UsageEvery and Anthropic
// hasn't said to wait; offline (--soak) never asks.
func (m *Model) fetchUsage() tea.Cmd {
	path := filepath.Join(state.Dir(), "usage.json")
	offline := m.offline
	var cmds []tea.Cmd
	for _, acct := range m.store.Config.AllAccounts() {
		acct := acct
		cmds = append(cmds, func() tea.Msg {
			return usageMsg{dir: acct.ConfigDir, u: claude.RefreshUsage(path, acct, offline)}
		})
	}
	return tea.Batch(append(cmds, m.fetchLoginUsage()...)...)
}

func (m *Model) scan() tea.Cmd {
	if m.scanning {
		return nil
	}
	m.scanning = true
	targets := m.targets()
	sc := m.scanner
	return func() tea.Msg { return scanMsg(sc.Run(targets)) }
}

// startDirs are the folders a new session can start in: where agtop was
// opened, then folders with agents running, then recent ones.
func (m *Model) startDirs() []string {
	seen := map[string]bool{}
	var out []string
	add := func(d string) {
		if d != "" && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	add(m.launchDir)
	agents := append([]*fleet.Agent(nil), m.snap.Agents...)
	sort.SliceStable(agents, func(i, j int) bool {
		if agents[i].Open() != agents[j].Open() {
			return agents[i].Open()
		}
		return agents[i].UpdatedAt.After(agents[j].UpdatedAt)
	})
	for _, a := range agents {
		if len(out) >= 12 {
			break
		}
		if (a.Interactive || a.Past) && strings.Contains(a.Cwd, "/var/folders/") {
			continue
		}
		add(a.Cwd)
	}
	return out
}

// dockLines is how many lines of the latest message the dock shows.
func (m *Model) dockLines() int {
	n := m.store.Config.DockLines
	if n == 0 {
		n = 3
	}
	return min(max(n, 1), max(1, m.h/3))
}

func (m *Model) startDir() string { return pickDir(m.startDirs(), m.dirIdx) }

func pickDir(dirs []string, i int) string {
	if len(dirs) == 0 {
		return ""
	}
	return dirs[((i%len(dirs))+len(dirs))%len(dirs)]
}

func (m *Model) targets() []fleet.Target {
	var targets []fleet.Target
	for _, a := range m.snap.Agents {
		if a.TranscriptPath != "" {
			targets = append(targets, fleet.Target{Key: a.Key, Path: a.TranscriptPath, Live: a.Live() || a.PID != 0, Past: a.Past})
		}
	}
	return targets
}

func (m *Model) rowAt(x, y int) string {
	i := y - m.listTop
	if m.mode != modeList || m.dialog != nil || m.picker != nil || m.sheet != nil || x >= m.listW || i < 0 || i >= len(m.rowKeys) {
		return ""
	}
	return m.rowKeys[i]
}

func (m *Model) mouseMove(x, y int) tea.Cmd {
	k := m.rowAt(x, y)
	if k == m.hover {
		return nil
	}
	m.hover = k
	// The row under the mouse is selected, as a click would, so moving
	// over to its Session doesn't take it back. A name being typed stays
	// with its agent.
	if k == "" || k == m.sel || strings.HasPrefix(k, "§") || m.inKind == inRename || m.inKind == inGroup {
		return nil
	}
	m.sel, m.armed = k, ""
	return m.loadPreview()
}

func (m *Model) mouseClick(x, y int) tea.Cmd {
	if y == m.listTop-1 && x < m.listW && m.mode == modeList && m.dialog == nil {
		if col := m.headerColumn(x); col != "" {
			m.setSort(col)
		}
		return nil
	}
	k := m.rowAt(x, y)
	if k == "" {
		return nil
	}
	double := k == m.sel && time.Since(m.lastClick) < 400*time.Millisecond
	m.sel, m.lastClick, m.armed = k, time.Now(), ""
	if strings.HasPrefix(k, "§") {
		m.toggleFold(strings.TrimPrefix(k, "§"))
		return nil
	}
	if double {
		return m.attach(m.selected())
	}
	return m.loadPreview()
}

func (m *Model) loadPreview() tea.Cmd {
	a := m.focused()
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
		return previewMsg{key: key, e: previewEntry{p: claude.ReadPreview(path, 384<<10), size: size}}
	}
}

// loadLivePreviews keeps the transcript tail of every working agent fresh, so
// rows can say what each one is doing right now.
func (m *Model) loadLivePreviews() tea.Cmd {
	var cmds []tea.Cmd
	for _, a := range m.snap.Agents {
		if !a.Live() || a.TranscriptPath == "" || len(cmds) >= 8 {
			continue
		}
		st, err := os.Stat(a.TranscriptPath)
		if err != nil {
			continue
		}
		if e, ok := m.previews[a.Key]; ok && e.size == st.Size() {
			continue
		}
		key, path, size := a.Key, a.TranscriptPath, st.Size()
		cmds = append(cmds, func() tea.Msg {
			return previewMsg{key: key, e: previewEntry{p: claude.ReadPreview(path, 128<<10), size: size}}
		})
	}
	return tea.Batch(cmds...)
}

// markSeen acknowledges an agent's question until it asks something new.
func (m *Model) markSeen(a *fleet.Agent) {
	if a == nil || a.State != "blocked" {
		return
	}
	m.store.Overlay.Seen[a.Key] = time.Now()
	_ = m.store.SaveOverlay()
}

func (m *Model) flash(s string, err bool) {
	m.status, m.statusErr, m.statusAt = s, err, time.Now()
	if err {
		m.react(fxError)
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.update(msg)
	m.applyJump()
	_, isTick := msg.(tickMsg)
	m.noteProgress(isTick)
	var copyCmd tea.Cmd
	if m.pendingCopy != "" {
		copyCmd, m.pendingCopy = tea.SetClipboard(m.pendingCopy), ""
	}
	var fxCmd tea.Cmd
	if m.fxKick {
		fxCmd, m.fxKick = fxTick(), false
	}
	return m, tea.Batch(cmd, copyCmd, fxCmd, m.syncLive(), m.syncHost(), m.syncWatch())
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if cmd, ok := m.barMsg(msg); ok {
		return m, cmd
	}
	switch msg := msg.(type) {
	case liveOpenMsg:
		return m, m.onLiveOpen(msg)
	case liveMsg:
		return m, m.onLive(msg)
	case hostOpenMsg:
		return m, m.onHostOpen(msg)
	case hostLinesMsg:
		return m, m.onHostLines(msg)
	case growMsg:
		m.onGrow(msg)
		return m, nil
	case subStatsMsg:
		m.onSubStats(msg)
		return m, nil
	case tempMsg:
		m.onTemp(msg)
		return m, nil
	case worktreesMsg:
		m.onWorktrees(msg)
		return m, nil
	case tidiedMsg:
		m.onTidied(msg)
		return m, nil
	case removedMsg:
		m.onRemoved(msg)
		return m, nil
	case squeezedMsg:
		m.onSqueezed(msg)
		return m, nil
	case cleanedMsg:
		m.onCleaned(msg)
		return m, nil
	case movedToAgtopMsg:
		// The old row is finished; the conversation carries on in agtop mode.
		m.store.Overlay.Done[msg.from] = time.Now()
		// Messages still waiting for the old row go to the new session.
		var carry tea.Cmd
		if q := m.localQ[msg.from]; q != nil && len(q.items) > 0 {
			text, id := strings.Join(q.items, "\n\n"), msg.started.id
			delete(m.localQ, msg.from)
			carry = cmdErr("queued messages moved over", func() error {
				// The new host may still be coming up.
				var c *host.Client
				var err error
				for range 30 {
					if c, err = host.Dial(id); err == nil {
						break
					}
					time.Sleep(100 * time.Millisecond)
				}
				if err != nil {
					return err
				}
				defer c.Close()
				return c.Send(text)
			})
		}
		if n := m.store.Overlay.Names[msg.from]; n != "" {
			m.store.Overlay.Names[state.Key(msg.started.acct, "a:"+msg.started.id)] = n
		}
		_ = m.store.SaveOverlay()
		if a := m.agentByKey(msg.from); a != nil && a.Interactive {
			defer m.flash(a.DisplayName+" carries on in agtop mode · its terminal copy is still open there, now under Done", false)
		}
		mm, cmd := m.update(msg.started)
		return mm, tea.Batch(cmd, carry)
	case screenDoneMsg:
		return m, m.onScreenDone(msg)
	case sheetMsg:
		return m, msg.apply(m)
	case rewoundMsg:
		return m, m.onRewound(msg)
	case hostStartedMsg:
		// Select the new session and give it the keys.
		m.refresh()
		m.sel = state.Key(msg.acct, "a:"+msg.id)
		m.rebuild()
		m.preview, m.paneFocus = true, true
		m.flash("started "+msg.name, false)
		return m, nil
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil
	case peekCheckMsg:
		return m, m.peekCheck()
	case tickMsg:
		m.tick++
		m.refresh()
		m.zenPick()
		m.followTail()
		cmds := []tea.Cmd{tick(), m.refreshSubs(), m.flushLocalQueues(), m.movePending(), m.measureTemp(), m.tidy(), m.squeezeTranscripts()}
		if m.mode == modeEff && !m.eff.loading && time.Since(m.eff.loaded) > 30*time.Second {
			cmds = append(cmds, m.effLoad(true)) // new transcript lines, every 30s while it's open
		}
		if m.mode == modeCleanup && time.Since(m.clean.checked) > 2*time.Minute {
			cmds = append(cmds, m.scanWorktrees()) // looked at when the view opens, and every 2 minutes while it's open
		}
		if m.tick%3 == 0 {
			cmds = append(cmds, m.scan())
		}
		if m.tick%60 == 0 {
			cmds = append(cmds, m.fetchUsage(), m.findLogins())
		}
		m.clkBeat(m.mood(m.tally()))
		if k := menubar.Goto(); k != "" && m.agentByKey(k) != nil {
			m.sel = k // a notification or the menu bar's menu was clicked
			m.rebuild()
		}
		cmds = append(cmds, m.loadPreview())
		if m.tick%2 == 0 {
			cmds = append(cmds, m.loadLivePreviews())
		}
		return m, tea.Batch(cmds...)
	case effLoadedMsg:
		m.onEffLoaded(msg)
		return m, nil
	case effRanMsg:
		return m, m.onEffRan(msg)
	case scanMsg:
		m.scanning = false
		m.loader.SetSpend(msg)
		m.loaded = true
		m.refresh()
		return m, nil
	case dialogReload:
		if m.dialog != nil {
			m.loadDialog()
		}
		if m.host != nil {
			m.host.mem = nil
		}
		if msg.err != nil {
			m.flash(msg.err.Error(), true)
		}
		return m, nil
	case usageMsg:
		m.loader.SetFetched(msg.dir, msg.u)
		m.refresh()
		return m, m.autoSwitch()
	case loginsMsg:
		return m, m.onLogins(msg)
	case switchedMsg:
		return m, m.onSwitched(msg)
	case addedLoginMsg:
		return m, m.onAddedLogin(msg)
	case fxTickMsg:
		return m, m.onFXTick()
	case subHoverMsg:
		// Redraw only if the run rested on is still the one under the pointer.
		if c := m.host; c == nil || !strings.HasPrefix(c.subHover, "sub:") || time.Since(c.subHoverAt) < subPeekAfter {
			m.sameFrame = true
		}
		return m, nil
	case previewMsg:
		m.previews[msg.key] = msg.e
		return m, nil
	case tea.FocusMsg:
		m.blurred = false
		return m, nil
	case tea.BlurMsg:
		m.blurred = true
		return m, nil
	case editedMsg:
		switch {
		case msg.err != nil:
			m.flash("editor: "+msg.err.Error(), true)
		case msg.pane && m.host != nil:
			c := m.host
			c.input, c.back = applyEdit(&c.pastes, c.input, msg.id, msg.text), 0
		case !msg.pane:
			m.input, m.back = applyEdit(&m.pastes, m.input, msg.id, msg.text), 0
		}
		return m, nil
	case localQueueFailed:
		// Back on the front of the queue, to try again when you say.
		// Back on the front of the queue; tried again in 30s, and held
		// only once it has failed three times running.
		if q := m.localQ[msg.key]; q != nil {
			q.items = append(msg.items, q.items...)
			q.fails++
			q.retry = time.Now().Add(30 * time.Second)
			if q.fails >= 3 {
				q.held, q.fails = true, 0
				m.flash("couldn't send the queue three times: "+msg.err.Error()+" · held; alt+h releases it", true)
				return m, nil
			}
		}
		m.flash("couldn't send the queue: "+msg.err.Error()+" · trying again in 30s", true)
		return m, nil
	case sendFailedMsg:
		m.sendFailed(msg)
		m.refresh()
		return m, nil
	case updateMsg:
		m.newer = msg.newer
		m.flash("agtop "+msg.newer.Short()+" is out · #update installs it", false)
		return m, nil
	case updatedMsg:
		m.updating = false
		if msg.err != nil {
			m.flash("update: "+msg.err.Error(), true)
		} else {
			m.newer = update.Info{}
			m.flash("agtop "+msg.to.Short()+" installed · reopen agtop to use it", false)
		}
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
	case jobGoneMsg:
		a := m.agentByKey(msg.key)
		if a == nil {
			return m, nil
		}
		return m, m.moveToAgtopWith(a, msg.text)
	case attachDoneMsg:
		m.attached = ""
		m.refresh()
		if msg.err != nil && msg.agent != nil {
			if daemon.IsRefusal(msg.err, "EKICKED") {
				m.flash("opened in another window", false)
				return m, nil
			}
			a := msg.agent
			if daemon.IsRefusal(msg.err, "ENOJOB") {
				// Claude Code has let the job go; its conversation carries on here.
				return m, m.moveToAgtop(a)
			}
			return m, tea.ExecProcess(actions.AttachFallback(a.Acct, a.ID), func(err error) tea.Msg {
				return doneMsg{err: err}
			})
		}
		return m, nil
	case clipImageMsg:
		switch {
		case msg.err != nil:
			m.flash("couldn't read the clipboard: "+msg.err.Error(), true)
		case msg.path == "":
			m.flash("no image on the clipboard", false)
		default:
			m.attachImages([]string{msg.path})
		}
		return m, nil
	case tea.PasteMsg:
		if !m.embedded {
			msg.Content = cleanPaste(msg.Content)
		}
		if m.embedded {
			m.embedPaste(msg.Content)
			return m, nil
		}
		if m.editingDoc() {
			m.host.memEd.insertText(msg.Content)
			return m, nil
		}
		// A paste with no text is what some terminals send when the
		// clipboard holds only an image: read the image itself.
		if strings.TrimSpace(msg.Content) == "" {
			return m, pasteClipImage()
		}
		// Image files dropped onto the terminal arrive as a paste of their
		// paths; they become attachments on whichever box has focus.
		if rest, imgs := extractImages(msg.Content); imgs != nil && m.dialog == nil {
			m.attachImages(imgs)
			if rest == "" {
				return m, nil
			}
			msg.Content = rest // the words around them go in as text
		}
		// A paste goes into whichever box has focus, at its cursor, newlines
		// kept so a pasted log or snippet arrives whole.
		// A long one shows as a chip and goes out whole.
		if c := m.host; c != nil && m.paneFocus {
			text := msg.Content
			if isLongPaste(text) {
				text = c.pastes.add(text)
			}
			pos := max(0, len(c.input)-c.back)
			c.undo.save(c.input, c.back, false)
			c.input = insert(c.input, pos, []rune(text))
			return m, nil
		}
		if m.acceptsText() {
			if m.dialog != nil {
				m.dialog.input = append(m.dialog.input, []rune(oneLine(msg.Content))...)
			} else {
				text := oneLine(msg.Content)
				if isLongPaste(msg.Content) {
					text = m.pastes.add(msg.Content)
				}
				m.input = insert(m.input, m.cursorPos(), []rune(text))
			}
		}
		return m, nil
	case tea.KeyPressMsg:
		return m, m.key(msg)
	case tea.MouseMotionMsg:
		// An open sheet has the mouse, as it has the keys.
		if m.sheet != nil {
			if msg.Button != tea.MouseLeft {
				m.sameFrame = true
				return m, m.pointerShape("default")
			}
			return m, tea.Batch(m.sheetMouse(mouseDrag, msg.X, msg.Y), m.pointerShape("grabbing"))
		}
		if c := m.host; c != nil && c.txt.drag {
			if msg.Button == tea.MouseLeft {
				m.dragTextSel(c, msg.X, msg.Y)
				return m, nil
			}
			m.endTextSel(c)
		}
		if m.boxDrag != 0 {
			if msg.Button == tea.MouseLeft {
				m.dragBox(msg.X, msg.Y)
				return m, nil
			}
			m.endBoxDrag()
		}
		if m.dragging {
			if msg.Button == tea.MouseLeft {
				m.dragSplit(msg.X)
				return m, nil
			}
			m.dragging = false
			_ = m.store.SaveConfig()
		}
		on := m.listW > 0 && m.mode == modeList && (msg.X == m.listW || msg.X == m.listW+1)
		hover := m.hover
		changed := on != m.divHover
		m.divHover = on
		cmd := m.mouseMove(msg.X, msg.Y)
		subChanged, subCmd := m.subMouseMove(msg.X, msg.Y)
		if !changed && !subChanged && m.hover == hover {
			m.sameFrame = true // nothing moved that shows: keep the last frame
		}
		want := "default"
		switch {
		case on:
			want = "ew-resize"
		case m.host != nil && m.host.subHover != "":
			want = "pointer" // a run to open, or the banner to go back
		}
		return m, tea.Batch(m.pointerShape(want), cmd, subCmd)
	case tea.MouseReleaseMsg:
		if m.sheet != nil {
			return m, tea.Batch(m.sheetMouse(mouseRelease, msg.X, msg.Y), m.pointerShape("default"))
		}
		if c := m.host; c != nil && c.txt.drag {
			m.endTextSel(c)
		}
		if m.boxDrag != 0 {
			m.endBoxDrag()
		}
		if m.dragging {
			m.dragging = false
			_ = m.store.SaveConfig()
		}
		return m, nil
	case tea.MouseClickMsg:
		if m.sheet != nil {
			if msg.Button == tea.MouseLeft {
				return m, m.sheetMouse(mousePress, msg.X, msg.Y)
			}
			return m, nil
		}
		if msg.Button == tea.MouseLeft && m.clickBox(msg.X, msg.Y) {
			return m, nil
		}
		// Grabbing the edge between list and pane resizes the list.
		if msg.Button == tea.MouseLeft && m.listW > 0 && m.mode == modeList && (msg.X == m.listW || msg.X == m.listW+1) {
			m.dragging = true
			return m, nil
		}
		if msg.Button == tea.MouseLeft && m.host != nil && (m.listW == 0 || msg.X > m.listW+1) && m.mode == modeList && m.dialog == nil {
			m.paneFocus = true // clicking the Session gives it the keys
			if m.viewName(m.host) == "screen" && m.canEmbed() {
				m.embedded = true
			}
			m.host.txt.on = false // a click elsewhere drops what was dragged over
			if m.clickBtw(m.host, msg.X, msg.Y) {
				return m, nil
			}
			if !m.embedded && m.startTextSel(m.host, msg.X, msg.Y) {
				return m, nil // a click on the text is one once it's released
			}
			m.clickRow(m.host, msg.Y)
			return m, nil
		}
		// Right-clicking a link in the Session asks what to do with it:
		// open it, in Preview, Quick Look, reveal or copy it.
		if msg.Button == tea.MouseRight && m.host != nil && (m.listW == 0 || msg.X > m.listW+1) && m.mode == modeList && m.dialog == nil && m.picker == nil && !m.embedded {
			m.linkMenu(m.host, msg.X, msg.Y)
			return m, nil
		}
		if msg.Button == tea.MouseLeft {
			m.paneFocus, m.embedded = false, false // clicking Agents takes the keys back
			return m, m.mouseClick(msg.X, msg.Y)
		}
	case tea.MouseWheelMsg:
		if m.sheet != nil {
			ev := mouseWheelDown
			if msg.Button == tea.MouseWheelUp {
				ev = mouseWheelUp
			}
			return m, m.sheetMouse(ev, msg.X, msg.Y)
		}
		// The wheel scrolls whatever is under the pointer: over the pane it
		// scrolls the conversation, and never moves the list behind it.
		if _, paneW, _ := m.layout(); m.mode == modeList && m.dialog == nil && paneW > 0 && (m.listW == 0 || msg.X > m.listW) {
			if c := m.host; c != nil {
				switch msg.Button {
				case tea.MouseWheelUp:
					c.scroll += 3
				case tea.MouseWheelDown:
					c.scroll = max(0, c.scroll-3)
				}
			}
			return m, nil
		}
		if m.mode == modeList && m.dialog == nil {
			switch msg.Button {
			case tea.MouseWheelUp:
				m.move(-3)
			case tea.MouseWheelDown:
				m.move(3)
			}
			return m, m.loadPreview()
		}
	}
	return m, nil
}

// pointerShape asks the terminal for the pointer's shape, when it's
// changed. OSC 22 sets it in terminals that support it (kitty, ghostty,
// wezterm, foot); others ignore it.
func (m *Model) pointerShape(want string) tea.Cmd {
	if want == m.pointer || (want == "default" && m.pointer == "") {
		return nil
	}
	m.pointer = want
	return tea.Raw("\x1b]22;" + want + "\x1b\\")
}

// viewNames are the places at the top: ctrl+\ moves between them, and tab
// moves within one (the list and its Session, or a place's pages).
var viewNames = []string{"Agents", "Efficiency", "Machine", "Settings"}

// The places, in viewNames' order.
const (
	placeAgents = iota
	placeEff
	placeMachine
	placeSettings
)

// machinePages and the Settings dialog's tabNames are the pages of the
// Machine and Settings places.
var machinePages = []string{"Processes", "Cleanup"}

// setView switches the whole screen to a place, on the page it was last on.
func (m *Model) setView(v int) {
	m.view = (v + len(viewNames)) % len(viewNames)
	m.dialog, m.mode, m.picker, m.sheet = nil, modeList, nil, nil
	m.input, m.inKind = m.input[:0], inPrompt
	m.zen = false
	switch m.view {
	case placeEff:
		m.setEffPage(m.eff.page)
	case placeMachine:
		m.setMachinePage(m.machinePage)
	case placeSettings:
		m.openDialog(m.settingsPage)
	}
}

// setMachinePage shows Processes (0) or Cleanup (1).
func (m *Model) setMachinePage(p int) {
	m.machinePage = (p + len(machinePages)) % len(machinePages)
	if m.machinePage == 0 {
		m.mode, m.procCursor, m.procPID = modeProcs, 0, 0
	} else {
		m.mode = modeCleanup
	}
}

// setSettingsPage shows one of the Settings dialog's tabs.
func (m *Model) setSettingsPage(p int) {
	m.settingsPage = (p + len(tabNames)) % len(tabNames)
	m.openDialog(m.settingsPage)
}

// setZen turns Zen on or off. Zen is Agents with only the agent that needs
// you on screen, the oldest first; the list comes back when it's off.
func (m *Model) setZen(on bool) {
	if on && m.view != placeAgents {
		m.setView(placeAgents)
	}
	m.zen, m.peek = on, zenPeek{}
	defer m.rebuild()
	if !on {
		return
	}
	m.embedded, m.full, m.paneFocus = false, false, true
	if q := m.zenQueue(); len(q) > 0 {
		m.sel = q[0].Key
	}
}

// zenFull is Zen showing only the agent, the whole screen.
func (m *Model) zenFull() bool { return m.zen }

// wide is when the preview gets its own half of the screen.
func (m *Model) wide() bool { return m.w >= 170 }

// autoSplit is when the Session shows beside the list without being asked:
// a wide screen you haven't pushed to the list alone.
func (m *Model) autoSplit() bool { return m.wide() && !m.store.Config.ListOnly }

func (m *Model) acceptsText() bool {
	return m.confirm == nil && m.sheet == nil && (m.dialog == nil || m.dialog.asking != "") && (m.mode == modeList || m.mode == modeCwd)
}

func (m *Model) refresh() {
	m.snap = m.loader.Load(true)
	m.notify()
	m.hibernate()
	m.reap()
	m.rebuild()
}

// notify posts a notification when an agent starts waiting on the user,
// and has clanker react to that and to agents answered, finished or failing.
func (m *Model) notify() {
	first := len(m.lastState) == 0
	if m.lastErr == nil {
		m.lastErr = map[string]bool{}
	}
	for _, a := range m.snap.Agents {
		if a.Checking {
			continue
		}
		prev := m.lastState[a.Key]
		m.lastState[a.Key] = a.State
		failing := strings.HasPrefix(a.Detail, "API error") || strings.HasPrefix(a.Detail, "usage limit")
		wasFailing := m.lastErr[a.Key]
		m.lastErr[a.Key] = failing
		if !first && prev != "" {
			switch {
			case failing && !wasFailing:
				m.react(fxError)
			case prev != "blocked" && a.NeedsYou():
				m.react(fxAsk)
			case prev == "blocked" && (a.State == "working" || a.State == "running"):
				m.react(fxAnswered)
			case (prev == "working" || prev == "running") && a.State == "done":
				m.react(fxDone)
			}
		}
		// Not for the agent you're looking at while agtop has focus.
		watching := !m.blurred && m.paneFocus && m.host != nil && m.host.key == a.Key
		// The menu bar icon, when it runs, notifies instead, with buttons.
		if !first && !m.store.Config.Quiet && prev != "" && prev != "blocked" && a.NeedsYou() && !watching && !menubar.Running() {
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
		if a.Worker != nil && a.State == "done" && a.Age(m.snap.At) > time.Duration(after)*time.Minute && !m.hibernated[a.Key] {
			m.hibernated[a.Key] = true // one try each; a failed stop is not retried every second
			go actions.Stop(a.Acct, a.ID, a.PID)
		}
	}
}

func (m *Model) agentByKey(key string) *fleet.Agent {
	for _, a := range m.snap.Agents {
		if a.Key == key {
			return a
		}
	}
	return nil
}

func (m *Model) selected() *fleet.Agent {
	for _, a := range m.order {
		if a.Key == m.sel {
			return a
		}
	}
	return nil
}

func (m *Model) move(d int) {
	items := m.items()
	if len(items) == 0 {
		return
	}
	i := 0
	for j, k := range items {
		if k == m.sel {
			i = j
		}
	}
	i = min(max(i+d, 0), len(items)-1)
	if m.sel != "" && !strings.HasPrefix(m.sel, "§") {
		m.shown = m.sel
	}
	m.sel = items[i]
	m.armed, m.hover = "", ""
}

// items are the rows ↑↓ stop on, in display order: the agents and every
// section heading, where enter folds or opens the section.
func (m *Model) items() []string {
	var out []string
	for _, l := range m.lines {
		switch l.kind {
		case lineSection:
			out = append(out, sectionKey(l.title))
		case lineAgent:
			out = append(out, l.agent.Key)
		}
	}
	return out
}

func (m *Model) folded(title string) bool {
	if v, ok := m.store.Config.Folds[title]; ok {
		return v
	}
	return title == "Earlier"
}

func (m *Model) toggleFold(title string) {
	if m.store.Config.Folds == nil {
		m.store.Config.Folds = map[string]bool{}
	}
	m.store.Config.Folds[title] = !m.folded(title)
	_ = m.store.SaveConfig()
	m.rebuild()
}

// focused is the agent whose card is open: the selection, or on a section
// heading the agent picked before it, so the Session beside the list
// doesn't come and go as ↑↓ pass a heading.
func (m *Model) focused() *fleet.Agent {
	key := m.sel
	if strings.HasPrefix(key, "§") {
		key = m.shown
	}
	for _, a := range m.order {
		if a.Key == key {
			m.shown = a.Key
			return a
		}
	}
	return nil
}

// rebuild groups the agents for the current group-by mode. What needs the
// user comes first; anything finished more than a day ago goes to Earlier.
func (m *Model) rebuild() {
	by := m.store.Config.GroupBy
	now := m.snap.At
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
	for _, a := range m.snap.Agents {
		if m.zen && !a.NeedsYou() && !a.Waiting() {
			continue // Zen's list is only the agents waiting on you
		}
		fresh := a.Open() || a.Busy() || a.Pinned || a.Age(now) < 24*time.Hour
		switch {
		case a.NeedsYou():
			add("Needs you", 0, a)
		case a.Waiting():
			add("Waiting on you", 3, a)
		case a.Checking || a.JustFinished(now):
			add("Working", 2, a)
		case a.Pinned:
			add("Pinned", 1, a)
		case a.Live() || a.Busy():
			add("Working", 2, a)
		case a.PID != 0:
			add("Idle", 5, a)
		case !fresh:
			add("Earlier", 9, a)
		case a.Done:
			add("Done", 8, a)
		case by == "repo":
			name := "No repository"
			if a.Repo != "" {
				name = filepath.Base(a.Repo)
				if a.Branch != "" {
					name += " · " + a.Branch
				}
			}
			add(name, 4, a)
		case by == "account":
			add(a.Acct.Name, 4, a)
		case by == "group" && a.Group != "":
			add(a.Group, 4, a)
		default:
			add("Today", 7, a)
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
	for _, g := range list {
		less := m.sortLess
		if g.name == "Done" {
			less = m.doneLess
		}
		sort.SliceStable(g.agents, func(i, j int) bool { return less(g.agents[i], g.agents[j]) })
	}
	m.order = m.order[:0]
	m.lines = m.lines[:0]
	clear(m.groupOf)
	if m.groupOf == nil {
		m.groupOf = map[string]string{}
	}
	for _, g := range list {
		var cost float64
		var names []string
		for _, a := range g.agents {
			cost += a.Spend.Cost
			if len(names) < 4 {
				names = append(names, oneLine(a.DisplayName))
			}
		}
		fold := m.folded(g.name)
		meta := sectionMeta(len(g.agents), cost)
		// One extra figure at most, and only one you can act on: temp work
		// where /clean all reaches it, memory where agents rest.
		switch g.name {
		case "Done", "Earlier":
			var temp int64
			for _, a := range g.agents {
				if a.PID == 0 {
					temp += a.Temp
				}
			}
			if temp >= tempShown {
				meta += " · " + disk(temp) + " tmp"
			}
		case "Idle":
			var held uint64
			for _, a := range g.agents {
				held += a.Mem
			}
			meta += " · " + mem(held) + " ram"
		}
		m.lines = append(m.lines, listLine{kind: lineSection, title: g.name, meta: meta,
			folded: fold, peek: strings.Join(names, ", ")})
		for _, a := range g.agents {
			m.order = append(m.order, a)
			m.groupOf[a.Key] = g.name
			if fold {
				continue
			}
			m.lines = append(m.lines, listLine{kind: lineAgent, agent: a})

		}
		m.lines = append(m.lines, listLine{kind: lineBlank})
	}
	valid := false
	for _, l := range m.lines {
		valid = valid || l.kind == lineSection && sectionKey(l.title) == m.sel || l.kind == lineAgent && l.agent.Key == m.sel
	}
	if !valid {
		if m.inKind == inReply && m.sel != "" {
			m.inKind = inPrompt
			m.flash("the agent you were replying to has gone", true)
		}
		m.sel = ""
		for _, k := range m.items() {
			if !strings.HasPrefix(k, "§") {
				m.sel = k
				break
			}
		}
		if items := m.items(); m.sel == "" && len(items) > 0 {
			m.sel = items[0]
		}
	}
}

var sortModes = []string{"name", "recent", "cost", "cpu", "ram", "time"}

// sortLess orders rows inside a section. By name a row keeps its place while
// its agent works; the number columns sort biggest first.
func (m *Model) sortLess(a, b *fleet.Agent) bool {
	now := m.snap.At
	byName := func() bool {
		x, y := strings.ToLower(a.DisplayName), strings.ToLower(b.DisplayName)
		if x != y {
			return x < y
		}
		return a.Key < b.Key
	}
	var x, y float64
	switch m.store.Config.SortBy {
	case "recent":
		x, y = float64(-a.Age(now)), float64(-b.Age(now))
	case "cost":
		x, y = a.Spend.Cost, b.Spend.Cost
	case "cpu":
		x, y = a.CPU, b.CPU
	case "ram":
		x, y = float64(a.Mem), float64(b.Mem)
	case "time":
		x, y = float64(a.Elapsed(now)), float64(b.Elapsed(now))
	default:
		return byName()
	}
	if x != y {
		return x > y
	}
	return byName()
}

// doneLess orders Done by when each was last touched, newest first: put
// away, or used since.
func (m *Model) doneLess(a, b *fleet.Agent) bool {
	last := func(a *fleet.Agent) time.Time {
		if t := m.store.Overlay.Done[a.Key]; t.After(a.UpdatedAt) {
			return t
		}
		return a.UpdatedAt
	}
	if x, y := last(a), last(b); !x.Equal(y) {
		return x.After(y)
	}
	return a.Key < b.Key
}

func (m *Model) setSort(mode string) {
	m.store.Config.SortBy = mode
	_ = m.store.SaveConfig()
	m.rebuild()
	m.flash("sorted by "+mode, false)
}

func sectionMeta(n int, cost float64) string {
	s := fmt.Sprintf("%d", n)
	if cost > 0 {
		s += " · " + money(cost)
	}
	return s
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
	m.attached = a.Key
	m.markSeen(a)
	if a.Interactive {
		m.flash(fmt.Sprintf("%s is open in another terminal (pid %d)", a.DisplayName, a.PID), false)
		return nil
	}
	if a.Past {
		m.flash(a.DisplayName+" is a past conversation · a message resumes it in agtop mode", false)
		return nil
	}
	// One attach at a time from here: the preview's would fight the full
	// screen over the session's size.
	m.closeLive()
	s := &daemon.Session{Client: daemon.Client{Account: a.Acct}, Short: a.ID}
	return tea.Exec(s, func(err error) tea.Msg { return attachDoneMsg{agent: a, err: err} })
}

func (m *Model) togglePin(a *fleet.Agent) tea.Cmd {
	pins, err := claude.LoadPins(a.Acct)
	if err != nil {
		m.flash(err.Error(), true)
		return nil
	}
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
	// The selection stays in the group the agent leaves: the row below it,
	// or above if it was the last. One whose process markDone is stopping
	// leaves when that exits, so it's counted as gone already, else the
	// selection would follow it down into Done.
	next, from := "", m.sectionOf(a.Key)
	if m.sel == a.Key {
		next = m.neighbour(a.Key)
	}
	stopping := !a.Done && a.PID != 0 && !a.Interactive && !a.Pinned
	defer func() {
		if next != "" && (stopping || m.sectionOf(a.Key) != from) && m.sectionOf(next) == from {
			m.sel = next
		}
	}()
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

// neighbour is the agent row after key in its section, or the one before
// when key is the last; "" when it's alone there.
func (m *Model) neighbour(key string) string {
	var rows []string
	i := -1
	for _, l := range m.lines {
		if l.kind == lineSection {
			if i >= 0 {
				break
			}
			rows = rows[:0]
		}
		if l.kind == lineAgent {
			if l.agent.Key == key {
				i = len(rows)
			}
			rows = append(rows, l.agent.Key)
		}
	}
	switch {
	case i < 0:
		return ""
	case i+1 < len(rows):
		return rows[i+1]
	case i > 0:
		return rows[i-1]
	}
	return ""
}

func (m *Model) nativeView() tea.Cmd {
	c := exec.Command("claude", "agents")
	c.Env = m.store.Config.ActiveAccount().Env()
	return tea.ExecProcess(c, func(err error) tea.Msg { return doneMsg{err: err} })
}
