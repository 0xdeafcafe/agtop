package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/rush/internal/actions"
	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/fleet"
	"github.com/0xdeafcafe/rush/internal/plugin"
	"github.com/0xdeafcafe/rush/internal/state"
)

func (m *Model) key(k tea.KeyPressMsg) tea.Cmd {
	// cmd arrives as super from terminals speaking the kitty protocol, and
	// as meta from those sending xterm's modifiers: both are cmd here.
	s := strings.ReplaceAll(k.String(), "meta+", "super+")
	m.hover = "" // the keyboard takes over from the mouse
	if m.host != nil {
		m.host.subHover = ""
	}
	m.lastKeyAt = time.Now()
	if cmd, used := m.remapKey(&k, &s); used {
		return cmd
	}
	if listToggleKey(s) && m.confirm == nil && m.sheet == nil && m.bar == nil && m.picker == nil && m.dialog == nil && m.mode == modeList && !m.zen {
		return m.toggleList()
	}
	if cmd, ok := m.hostedKeyGuard(s); ok {
		return cmd
	}
	if m.hosted != "" {
		// No command bar: it reaches every agent and place.
	} else if cmd, ok := m.barToggle(s); ok {
		return cmd
	}
	if m.bar != nil && s != "ctrl+q" {
		return m.barKey(k, s)
	}
	if m.embedded {
		return m.embedKey(k)
	}
	if s == "ctrl+q" {
		if c := m.host; c != nil && c.memEd != nil && c.memEd.dirty() && m.confirm == nil {
			m.confirm = &confirmation{
				question: "Quit with unsaved changes to " + tildify(c.memEd.path) + "?",
				detail:   "they're lost",
				onYes: func() tea.Cmd {
					return m.quit()
				},
			}
			return nil
		}
		return m.quit()
	}
	if m.confirm != nil {
		return m.confirmKey(s)
	}
	if m.sheet != nil {
		return m.sheet.key(m, k, s)
	}
	// A file being edited in the memory view takes every key, < > tab and
	// ctrl+z included; esc hands them back.
	if m.editingDoc() {
		return m.paneKey(k, s)
	}
	// A Session filling a narrow screen has the only box there is, so the
	// keys are its: never typing into a Prompt that isn't drawn.
	if m.host != nil && m.listW == 0 && (m.preview || m.full) && m.mode == modeList && m.dialog == nil && !m.zen {
		m.paneFocus = true
	}
	if m.picker != nil {
		return m.pickerKey(k, s)
	}
	// , and . (or < and >) with nothing typed go to the previous and next
	// place (ctrl+\
	// still goes to the next), ctrl+z turns Zen on and off, from anywhere
	// but a question being asked.
	if d := m.placeStep(s); d != 0 {
		m.setView(m.view + d)
		if m.hosted != "" && m.view == placeProjects {
			m.setView(m.view + d) // hosted is one session: no other projects to go to
		}
		return tea.Batch(m.loadPreview(), m.effOpen(), m.projectsOpen())
	}
	if s == "alt+w" && (m.dialog == nil || m.dialog.asking == "") {
		// Which profile, or which provider, new sessions run.
		m.openProfilePicker()
		return nil
	}
	if s == "ctrl+z" && (m.dialog == nil || m.dialog.asking == "") {
		m.setZen(!m.zen)
		return m.loadPreview()
	}
	// In zen, tab held down peeks at what's working; letting go (or,
	// tapped, any key) comes back.
	if m.peek.on {
		return m.peekKey(s)
	}
	if s == "tab" && m.zenFull() {
		m.peek = zenPeek{on: true, at: time.Now()}
		return nil
	}
	// In zen, while the next agent connects (or when nothing needs you),
	// keys wait rather than land in some box you can't see; ctrl+n still
	// moves on.
	if m.zenFull() && (m.host == nil || len(m.zenQueue()) == 0) {
		switch s {
		case "tab", "ctrl+q", "ctrl+c", "?":
		case "ctrl+n":
			m.zenSkip()
			return nil
		default:
			return nil
		}
	}
	if m.paneFocus && m.host != nil && m.mode == modeList && m.dialog == nil && s == "ctrl+c" {
		return m.paneKey(k, s)
	}
	if s == "ctrl+c" {
		if m.anchor > 0 && m.anchor-1 != m.cursorPos() {
			m.editInput(k, s) // copies the selection
			return nil
		}
		if len(m.input) > 0 {
			m.clearPrompt()
			return nil
		}
		return m.quitKey()
	}
	// On a Claude Code agent's screen, typing goes into it: ← moves its
	// cursor rather than leaving. tab, { } [ ] and ctrl+] stay rush's.
	if !m.zen && m.paneFocus && m.host != nil && m.mode == modeList && m.dialog == nil && m.viewName(m.host) == "screen" && m.canEmbed() {
		switch s {
		case "tab", "{", "}", "shift+tab", "[", "]", "ctrl+]":
		default:
			m.embedded = true
			return m.embedKey(k)
		}
	}
	// ⌥m (shift+tab in the list) picks what the next session starts as wherever the box says so,
	// the Session focused too; a queued message selected keeps it, to merge.
	if s == "alt+m" && m.mode == modeList && m.dialog == nil && (m.host == nil || !strings.HasPrefix(m.host.sel, "q:")) {
		return m.openStartSheet()
	}
	if m.paneFocus && m.host != nil && m.mode == modeList && m.dialog == nil && s != "tab" {
		return m.paneKey(k, s)
	}
	// Renaming, tab and ↑↓ save the name and go on to rename the next
	// agent, as in the Finder.
	if m.inKind == inRename && m.mode == modeList && m.dialog == nil {
		if d := map[string]int{"tab": 1, "down": 1, "shift+tab": -1, "up": -1}[s]; d != 0 {
			return m.renameStep(d)
		}
	}
	// In Agents tab goes between the list and the Session, unless a
	// command is being typed: then it completes it.
	if s == "tab" && m.mode == modeList && m.dialog == nil {
		if c := m.host; m.paneFocus && c != nil {
			if cmd, used := m.slashKey(c, s); used {
				return cmd
			}
		} else if cmd, used := m.fleetSlashKey(s); used {
			return cmd
		}
		return m.switchFocus()
	}
	// Where there's no list and Session to go between, tab and shift+tab
	// turn the page, as ] and [ do.
	if m.onPages() {
		switch s {
		case "tab":
			s = "]"
		case "shift+tab":
			s = "["
		}
	}
	// [ and ] go through a place's pages, as everywhere with pages: in
	// Efficiency unless a note is being written or an install waits on an
	// answer.
	if (s == "[" || s == "]") && m.mode == modeEff && !m.eff.typing() && m.eff.plan == nil {
		d := 1
		if s == "[" {
			d = -1
		}
		m.setEffPage(m.eff.page + d)
		return m.effOpen()
	}
	if (s == "[" || s == "]") && m.mode == modeWall {
		d := 1
		if s == "[" {
			d = -1
		}
		m.setAgentsPage(m.work.page + d)
		return m.refreshFolders()
	}
	if m.dialog != nil {
		return m.dialogKey(k, s)
	}
	switch m.mode {
	case modeHelp:
		switch s {
		case "]", "right", "l":
			m.helpPage = (m.helpPage + 1) % len(helpPages)
		case "[", "left", "h":
			m.helpPage = (m.helpPage + len(helpPages) - 1) % len(helpPages)
		case "1", "2", "3":
			m.helpPage = int(s[0] - '1')
		default:
			m.mode = modeList
		}
		return nil
	case modeEff:
		return m.effKey(k, s)
	case modeProjects:
		return m.projectsKey(s)
	case modeWall:
		return m.wallKey(s)
	}
	return m.listKey(k, s)
}

// placeStep is which way a key moves between places: -1 for , and <, +1
// for . > and ctrl+\, 0 when it doesn't. , . < > move only from the list
// with nothing typed in its box, and never while a Session's box has the
// keys, so they can always be typed there.
func (m *Model) placeStep(s string) int {
	d := map[string]int{",": -1, "<": -1, ".": 1, ">": 1, "ctrl+\\": 1}[s]
	if d == 0 || m.dialog != nil && m.dialog.asking != "" || m.mode == modeEff && m.eff.typing() {
		return 0
	}
	if s == "ctrl+\\" || m.mode != modeList || m.dialog != nil {
		return d
	}
	if m.paneFocus && m.host != nil {
		// A Session's box types them, first character or not: a message
		// may start with a > quote. ctrl+\ still moves.
		return 0
	}
	if len(m.input) > 0 || m.inKind != inPrompt {
		return 0
	}
	return d
}

// switchFocus is tab, { or } in Agents: from the list into the selected agent's
// Session, and back.
func (m *Model) switchFocus() tea.Cmd {
	if m.paneFocus && m.host != nil {
		m.leavePane()
		return nil
	}
	a := m.selected()
	if a == nil || strings.HasPrefix(m.sel, "§") {
		return nil
	}
	if a.Rush {
		return m.focusPane(a)
	}
	if m.canEmbed() {
		m.embedded = true
		return nil
	}
	m.preview = true
	return m.loadPreview()
}

func (m *Model) listKey(k tea.KeyPressMsg, s string) tea.Cmd {
	if m.listFilter != nil {
		if cmd, used := m.listFilterKey(k, s); used {
			return cmd
		}
	}
	a := m.selected()
	empty := len(m.input) == 0
	if s == "ctrl+v" && m.acceptsText() && m.dialog == nil {
		return pasteClipImage()
	}
	if cmd, used := m.fleetSlashKey(s); used {
		return cmd
	}
	switch s {
	case "up":
		m.move(-1)
		return m.loadPreview()
	case "down":
		m.move(1)
		return m.loadPreview()
	case "pgup":
		m.move(-10)
		return m.loadPreview()
	case "pgdown":
		m.move(10)
		return m.loadPreview()
	case "home", "shift+up":
		if empty || s == "shift+up" {
			m.move(-len(m.lines))
			return m.loadPreview()
		}
	case "end", "shift+down":
		if empty || s == "shift+down" {
			m.move(len(m.lines))
			return m.loadPreview()
		}
	case "right":
		if !empty && a != nil && !strings.HasPrefix(m.sel, "§") && m.cursorPos() == len(m.input) && m.edgePush("list-right") {
			m.confirm = &confirmation{
				question: "Open " + a.DisplayName + "?",
				detail:   "what you typed stays in this box",
				onYes:    func() tea.Cmd { return m.focusPane(a) },
				again:    "right",
			}
			return nil
		}
		if empty && a != nil && !strings.HasPrefix(m.sel, "§") {
			return m.focusPane(a)
		}
		if empty {
			if t, ok := strings.CutPrefix(m.sel, "§"); ok {
				if m.folded(t) {
					m.toggleFold(t)
				}
				return nil
			}
			if m.canEmbed() {
				m.embedded = true
				return nil
			}
			// On a narrow screen the preview is already full width.
			if (m.preview || m.autoSplit()) && m.w >= 120 {
				m.preview, m.full = true, true
			} else {
				m.preview = true
			}
			return m.loadPreview()
		}
	case "left":
		// In Agents, ← has nothing to go back to but folds a heading it's on.
		if empty {
			if t, ok := strings.CutPrefix(m.sel, "§"); ok && !m.folded(t) {
				m.toggleFold(t)
			}
			return nil
		}
	case "esc":
		switch {
		case !empty:
			m.clearPrompt()
			if m.inKind != inReply {
				m.inKind = inPrompt
			}
		case m.inKind != inPrompt:
			m.inKind = inPrompt
		case m.peeking() && m.agentByKey(m.peekFrom) != nil:
			m.sel = m.peekFrom
			return m.openPeeked()
		case m.preview:
			m.leaveChat()
		case time.Since(m.quitArmed) < 2*time.Second:
			return m.quit()
		default:
			m.quitArmed = time.Now()
			m.flash("esc again to quit", false)
		}
		return nil
	case "enter":
		if t, ok := strings.CutPrefix(m.sel, "§"); ok && empty {
			m.toggleFold(t)
			return nil
		}
		// Peeking from a Session, enter opens the one picked, as it was.
		if empty && a != nil && m.inKind == inPrompt && m.peeking() {
			return m.openPeeked()
		}
		// Enter on an agent renames it, as in the Finder, or opens it, as
		// you chose the first time; ⌘↓, →, tab and { } always open it.
		if empty && a != nil && m.inKind == inPrompt {
			switch m.store.Config.EnterOn {
			case "open":
				return m.focusPane(a)
			case "rename":
				m.startRename(a)
			default:
				m.askEnter(a)
			}
			return nil
		}
		return m.submit()
	case "ctrl+r":
		switch {
		case a == nil:
			m.flash("select an agent first", true)
		case !empty && m.inKind == inPrompt:
			m.flash("finish or clear what's typed first (esc)", true)
		default:
			m.startRename(a)
		}
		return nil
	case "shift+tab", "alt+m":
		// What the next session starts as: agent, model and effort.
		if m.inKind == inPrompt {
			return m.openStartSheet()
		}
	case "super+down":
		// ⌘↓ opens, as in the Finder, into the agent's Session message box,
		// when there's nothing typed; typing something, it's the box's own
		// key, to the end of the text.
		if empty {
			if a != nil && !strings.HasPrefix(m.sel, "§") {
				return m.focusPane(a)
			}
			return nil
		}
	case "ctrl+l":
		// In the list's Prompt, typed or not: where new sessions start.
		// Moving the selected agent is #cd.
		if m.inKind == inPrompt && !isHashCmd(string(m.input)) || a == nil {
			m.openDirPicker()
			return nil
		}
		m.openMovePicker(a)
		return nil
	case "alt+l":
		// New sessions from an agent in a worktree: its main checkout or
		// the worktree itself.
		m.startInTree = !m.startInTree
		m.flash("new sessions start in "+tildify(m.startDir()), false)
		return nil
	case "ctrl+y":
		return m.openPR(a)
	case "ctrl+t":
		if a != nil {
			return m.togglePin(a)
		}
	case "ctrl+s":
		m.cycleGroupBy()
		return nil
	case "ctrl+p":
		m.toggleSplit()
		return m.refreshFolders()
	case "ctrl+o":
		// Reply: straight into the agent's Session message box.
		if a != nil {
			return m.focusPane(a)
		}
		return nil
	case "ctrl+x":
		return m.stopOrRemove(a)
	case "ctrl+d", "alt+d":
		// Done with it: to Done, its idle process stopped.
		return m.markDone(a)
	case "ctrl+b", "alt+g":
		// Go on: what "keep going" in its message box would do.
		return m.keepGoing(a)
	case "{", "}":
		// With nothing typed, into the Session, as tab does.
		if empty {
			return m.switchFocus()
		}
	case "[", "]":
		// [ ] step through Agents' own pages, the list and the Wall, as they
		// do everywhere else there are pages; { into a live session
		// first (paneKey has its own [ ] for the views inside it) to
		// cycle what its preview shows without leaving the list.
		if empty {
			d := 1
			if s == "[" {
				d = -1
			}
			m.setAgentsPage(m.work.page + d)
			return m.refreshFolders()
		}
	case "v":
		// A Claude Code agent's Session, previewed without tabbing in:
		// v swaps its live screen for its summary, since [ ] now steps
		// through Agents' own pages instead.
		if empty && a != nil {
			m.claudeView = 1 - m.claudeView
			m.preview = true
			return nil
		}
	case "ctrl+n":
		return m.nextNeedingYou()
	case "shift+left", "shift+right", "alt+left", "alt+right":
		if empty {
			if cmd, ok := m.stepSplit(strings.HasSuffix(s, "right")); ok {
				return cmd
			}
		}
	case "?":
		if empty {
			m.mode = modeHelp
			m.didStep("keys")
			return nil
		}
	case "alt+f":
		if empty && m.view == placeAgents {
			m.startListFilter()
			return nil
		}
	}
	if s == "ctrl+g" {
		return editDraft(&m.pastes, m.input, false)
	}
	if m.inKind == inPrompt && (isUndo(s) || isRedo(s)) {
		u := m.undo.undo
		if isRedo(s) {
			u = m.undo.redo
		}
		if buf, back, ok := u(m.input, m.back); ok {
			m.input, m.back, m.anchor = buf, back, 0
		}
		return nil
	}
	before := string(m.input)
	m.editInput(k, s)
	if s == "space" && (m.inKind == inPrompt || m.inKind == inReply) && m.anchor == 0 {
		// A path to an image, typed or dropped in as keys, becomes an
		// attachment once it's done.
		if t, ok := m.imgs.inline(string(m.input), m.lookPath); ok {
			m.input = []rune(t)
			m.setCursor(len(m.input))
		}
	}
	if string(m.input) != before {
		m.slashSel = 0
	}
	return nil
}

// startRename puts the agent's name in the box, all of it selected so
// typing replaces it.
func (m *Model) startRename(a *fleet.Agent) {
	m.didStep("rename")
	name := []rune(a.DisplayName)
	m.inKind, m.input, m.back, m.promptFor = inRename, name, 0, a.Key
	m.anchor = 0
	if len(name) > 0 {
		m.anchor = 1 // from the start to the cursor at the end
	}
}

// renameStep saves the name being typed and renames the agent d rows on,
// skipping section headers; past either end it stays on the last one.
func (m *Model) renameStep(d int) tea.Cmd {
	from := m.promptFor
	m.submit()
	m.anchor = 0
	items := m.items()
	i := -1
	for j, k := range items {
		if k == from {
			i = j
		}
	}
	if i < 0 {
		return nil
	}
	for j := i + d; j >= 0 && j < len(items); j += d {
		if strings.HasPrefix(items[j], "§") {
			continue
		}
		m.sel = items[j]
		if a := m.selected(); a != nil {
			m.startRename(a)
		}
		return m.loadPreview()
	}
	return nil
}

// nextNeedingYou selects the agent that has waited longest for you, so a
// stack of questions can be cleared with ctrl+n and an answer each.
func (m *Model) nextNeedingYou() tea.Cmd {
	var best *fleet.Agent
	for _, a := range m.order {
		if a.NeedsYou() && (best == nil || a.UpdatedAt.Before(best.UpdatedAt)) {
			best = a
		}
	}
	if best == nil {
		m.flash("nothing needs you", false)
		return nil
	}
	m.didStep("next")
	m.sel = best.Key
	m.rebuild()
	return m.loadPreview()
}

// quit ends the view once the spend read so far is saved, off the UI
// goroutine: the next start needn't read it all again.
func (m *Model) quit() tea.Cmd {
	sc := m.scanner
	return func() tea.Msg { sc.Flush(); return tea.QuitMsg{} }
}

// quitKey arms quitting on the first ctrl+c and quits on a second one.
func (m *Model) quitKey() tea.Cmd {
	if time.Since(m.quitArmed) < 2*time.Second {
		return m.quit()
	}
	m.quitArmed = time.Now()
	m.flash("ctrl+c again to quit", false)
	return nil
}

func (m *Model) sectionOf(key string) string {
	cur := ""
	for _, l := range m.lines {
		if l.kind == lineSection {
			cur = sectionKey(l.title)
		}
		if l.kind == lineAgent && l.agent.Key == key {
			return cur
		}
	}
	return key
}

func (m *Model) cycleGroupBy() {
	cur := m.store.Config.GroupBy
	modes := m.groupModes()
	next := modes[0]
	for i, g := range modes {
		if g == cur {
			next = modes[(i+1)%len(modes)]
		}
	}
	m.store.Config.GroupBy = next
	_ = m.store.SaveConfig()
	m.flash("grouped by "+m.groupLabel(next), false)
	m.rebuild()
}

func (m *Model) stopOrRemove(a *fleet.Agent) tea.Cmd {
	if a == nil {
		return nil
	}
	if a.Past {
		m.flash(a.DisplayName+" is a past conversation: nothing runs to stop, and its transcript is kept", false)
		return nil
	}
	if a.Interactive {
		pid := a.PID
		m.confirm = &confirmation{
			question: "Close " + a.DisplayName + "?",
			detail:   fmt.Sprintf("sends SIGTERM to the terminal session (pid %d)", pid),
			onYes: func() tea.Cmd {
				return cmdErr("closed "+a.DisplayName, func() error { return actions.Terminate(pid) })
			},
		}
		return nil
	}
	if a.PID != 0 || (a.Live() && a.Worker != nil) {
		m.flash("stopping "+a.DisplayName+"…", false)
		return cmdErr("stopped "+a.DisplayName, func() error { return stopOutside(a) })
	}
	m.confirm = &confirmation{
		question: "Delete " + a.DisplayName + "?",
		detail:   "and its worktree, when that's safe",
		onYes: func() tea.Cmd {
			m.flash("deleting "+a.DisplayName+"…", false)
			return cmdErr("deleted "+a.DisplayName, func() error { return removeOutside(a) })
		},
	}
	return nil
}

// replyTo sends text to an agent however it takes messages: its host, a
// resume into rush mode, its queue while it's busy, or Claude Code.
// tagged is the text with its pastes marked, for rush sessions.
func (m *Model) replyTo(a *fleet.Agent, text, tagged string) tea.Cmd {
	m.markSeen(a)
	m.flash("sending to "+a.DisplayName+"…", false)
	m.loader.Nudge(a.Key)
	m.refresh()
	if a.Rush {
		return sendHosted(a, tagged)
	}
	text = m.imgs.paths(text)
	m.imgs = imageRefs{}
	if a.Past {
		return m.moveToRushWith(a, text)
	}
	if q := m.localQ[a.Key]; busy(a) || q != nil && len(q.items) > 0 {
		m.queueLocal(a.Key, text)
		m.flash(fmt.Sprintf("queued for %s · goes within 15s", a.DisplayName), false)
		return nil
	}
	return reply(a, text)
}

// keepGoing tells an agent that stopped to go on: "keep going" after a
// finished turn, "continue" after an error.
func (m *Model) keepGoing(a *fleet.Agent) tea.Cmd {
	switch {
	case a == nil:
		m.flash("select an agent first", true)
		return nil
	case a.Interactive:
		m.flash(a.DisplayName+" is open in another terminal", true)
		return nil
	case a.Live() || a.Busy():
		m.flash(a.DisplayName+" is still working", false)
		return nil
	}
	return m.replyTo(a, a.ContinueText(), a.ContinueText())
}

func (m *Model) submit() tea.Cmd {
	text := strings.TrimSpace(m.pastes.expand(string(m.input), false))
	tagged := strings.TrimSpace(m.pastes.expand(string(m.input), true)) // for rush sessions
	kind := m.inKind
	a := m.selected()
	if kind == inRename || kind == inGroup {
		a = m.agentByKey(m.promptFor) // the agent the prompt was opened for
	}
	if kind == inReply && (a == nil || a.Interactive) {
		m.flash("pick the agent to reply to first (↑↓)", true)
		return nil
	}
	// The open pane is what knows the conversation's cache; the box is
	// left as it is while you're asked.
	if kind == inReply && text != "" && m.host != nil && m.host.key == a.Key && m.askCold(m.host, text, m.submit) {
		return nil
	}
	m.pastes, m.undo = pastes{}, undoStack{}
	m.input, m.inKind = m.input[:0], inPrompt
	if (kind == inPrompt || kind == inReply) && text != "" && !isHashCmd(text) {
		m.emitBox(plugin.EvInputSent, "", tagged) // for the history's Sent
	}
	switch kind {
	case inRename:
		if a == nil {
			return nil
		}
		if text == "" || text == a.Name {
			delete(m.store.Overlay.Names, a.Key)
		} else {
			m.store.Overlay.Names[a.Key] = text
		}
		_ = m.store.SaveOverlay()
		m.refresh()
		return nil
	case inGroup:
		if a == nil {
			return nil
		}
		if text == "" {
			delete(m.store.Overlay.Groups, a.Key)
		} else {
			m.store.Overlay.Groups[a.Key] = text
			if m.store.Config.GroupBy != "group" {
				m.store.Config.GroupBy = "group"
				_ = m.store.SaveConfig()
			}
		}
		_ = m.store.SaveOverlay()
		m.refresh()
		return nil
	}
	if kind == inReply {
		if a == nil || text == "" {
			return nil
		}
		m.inKind = inReply
		return m.replyTo(a, text, tagged)
	}
	if text == "" {
		return m.attach(a)
	}
	if cmd, ok := m.sendMentioned(text, tagged); ok {
		return cmd
	}
	if to := m.mentionsIn(text); len(to) > 0 && !isHashCmd(text) {
		return m.replyTo(to[0], text, tagged) // tagged later in the message: it goes to them all the same
	}
	if isHashCmd(text) {
		return m.command(a, text)
	}
	if !strings.HasPrefix(text, "/") {
		m.didStep("start")
	}
	if cmd, ok := m.setupCommand(nil, text, tagged); ok {
		return cmd
	}
	if strings.HasPrefix(text, "/") {
		if cmd, ok := m.legacyCommand(text); ok {
			return cmd
		}
	}
	// The Prompt only starts new sessions; replies go through a Session's
	// own message box.
	if d := m.store.Config.Dispatch; m.startOver != nil || m.startKind() != state.LoginsKind || d.RunIn != "daemon" && d.OwnAgent() {
		return m.startHosted(tagged, m.startDir())
	}
	d, ok := agent.As[agent.Dispatcher](loginsKind)
	if !ok {
		return m.startHosted(tagged, m.startDir())
	}
	acct := m.store.Config.ActiveAccount().Profile()
	dir := m.startDir()
	flags := m.store.Config.Dispatch.Flags()
	m.flash("starting a new session…", false)
	return func() tea.Msg {
		id, err := d.Dispatch(acct, dir, text, flags...)
		if err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{text: "started " + id}
	}
}

// command runs one of rush's # commands on agent a: the selected one from
// the Prompt, the Session's own from its box.
func (m *Model) command(a *fleet.Agent, text string) tea.Cmd {
	f := strings.Fields(text)
	name, arg := strings.ToLower(strings.TrimLeft(f[0], "#/")), strings.TrimSpace(strings.TrimPrefix(text, f[0]))
	// #view:list is #view list.
	if n, r, ok := strings.Cut(name, ":"); ok && arg == "" {
		name, arg = n, r
	}
	if n := fleetAliases[name]; n != "" {
		name = n
	}
	if m.isPluginCommand(name) {
		return m.runPluginCommand(name, a)
	}
	need := func() bool {
		if a == nil {
			m.flash("select an agent first", true)
			return false
		}
		return true
	}
	m.didStep("hash")
	switch name {
	case "stash":
		return m.stashCommand("history")
	case "tips":
		o := &m.store.Config.Onboarding
		if strings.TrimSpace(arg) == "off" {
			o.Hidden = true
			_ = m.store.SaveConfig()
			m.flash("Getting started put away · #tips brings it back", false)
			return nil
		}
		o.Hidden, o.Steps, o.Tips = false, nil, nil
		_ = m.store.SaveConfig()
		m.flash("Getting started and tips from the top", false)
	case "done":
		return m.markDone(a)
	case "go":
		if need() {
			return m.keepGoing(a)
		}
	case "clean":
		if strings.TrimSpace(arg) == "all" {
			return m.askCleanAll()
		}
		if need() {
			return m.askClean(a)
		}
	case "stop":
		if need() {
			if a.Past {
				return m.stopOrRemove(a)
			}
			return cmdErr("stopped "+a.DisplayName, func() error { return stopOutside(a) })
		}
	case "rm":
		if need() && a.Past {
			return m.stopOrRemove(a)
		}
		if need() {
			m.confirm = &confirmation{
				question: "Delete " + a.DisplayName + "?",
				detail:   "removes the session, and its worktree when that's safe",
				onYes: func() tea.Cmd {
					return cmdErr("deleted "+a.DisplayName, func() error { return removeOutside(a) })
				},
			}
		}
	case "kill":
		if need() {
			m.askKillTree(a)
		}
	case "restart":
		if need() {
			return m.restart(a, arg)
		}
	case "slim":
		m.openSlim(m.host)
	case "compact":
		if need() {
			return m.openCompact(m.host, a)
		}
	case "cd":
		if need() {
			if expand(arg) == "" {
				m.openMovePicker(a)
				return nil
			}
			return m.moveTo(a, expand(arg))
		}
	case "account":
		if arg == "" {
			m.openAgentSettings(agent.Kind(m.startKind()))
			return nil
		}
		return m.useLogin(arg)
	case "group":
		if need() {
			m.inKind, m.input, m.promptFor = inGroup, []rune(arg), a.Key
			return m.submit()
		}
	case "split":
		switch arg {
		case "":
			m.toggleSplit()
		case "project", "none":
			if (arg == "project") != m.splitProjects() {
				m.toggleSplit()
			}
		default:
			m.flash("split by project or none", true)
			return nil
		}
		return m.refreshFolders()
	case "by":
		for _, g := range m.groupModes() {
			if g == arg {
				m.store.Config.GroupBy = g
				_ = m.store.SaveConfig()
				m.rebuild()
				return nil
			}
		}
		m.flash("group by one of: "+strings.Join(m.groupModes(), ", "), true)
	case "rename":
		if need() {
			if arg == "" {
				m.startRename(a)
				return nil
			}
			m.inKind, m.input, m.promptFor = inRename, []rune(arg), a.Key
			return m.submit()
		}
	case "sort":
		for _, mode := range sortModes {
			if mode == arg {
				m.setSort(mode)
				return nil
			}
		}
		m.flash("sort by one of: "+strings.Join(sortModes, ", "), true)
	case "native":
		return m.nativeView()
	case "view":
		switch arg {
		case "split":
			return m.splitAgain()
		case "agent", "chat", "session":
			return m.sessionOnly()
		case "list", "agents", "orchestrator":
			m.listOnly()
		default:
			m.flash("view one of: split, agent, list", true)
		}
	case "width":
		var pct float64
		if _, err := fmt.Sscanf(strings.TrimSuffix(arg, "%"), "%g", &pct); err != nil || pct <= 0 {
			m.store.Config.SideWidth = 0
			_ = m.store.SaveConfig()
			m.flash("list width back to rush's choice · /width 30% sets your own", false)
			return nil
		}
		m.setSideWidth(int(pct / 100 * float64(m.w)))
	case "rush":
		if need() {
			return m.moveToRush(a)
		}
	case "with":
		m.withAgent(arg)
	case "profile":
		m.usePickedProfile(arg)
	case "hibernate":
		var n int
		fmt.Sscanf(arg, "%d", &n)
		m.store.Config.Hibernate.AfterMinutes = n
		_ = m.store.SaveConfig()
		if n > 0 {
			m.flash(fmt.Sprintf("finished agents stop after %dm idle", n), false)
		} else {
			m.flash("hibernation off", false)
		}
	case "update":
		return m.installUpdate()
	case "reload":
		return m.reload()
	case "ask":
		return m.askRush(arg)
	case "help":
		m.mode = modeHelp
		m.didStep("keys")
	case "quit":
		return m.quit()
	case "pin":
		if need() {
			m.didStep("pin")
			return m.togglePin(a)
		}
	case "pr":
		if need() {
			return m.openPR(a)
		}
	case "full":
		if need() {
			if a.Rush || a.Interactive || a.Past {
				m.flash("only a Claude Code agent in the background opens full screen", true)
				return nil
			}
			return m.attach(a)
		}
	case "folder":
		m.openDirPicker()
	case "advisor":
		return m.advCommand(arg)
	case "mackeys":
		return m.macKeysCommand(arg)
	case "statusline":
		m.openTopBar(a)
	case "network":
		m.sheet = &netSheet{}
	case "efficiency":
		// It reads the transcripts of the accounts rush switches between.
		if !agent.Supports(loginsKind, agent.FeatureEfficiency) {
			m.flash(agentName(string(loginsKind))+"'s efficiency isn't something rush reads yet", true)
			return nil
		}
		m.setView(placeEff)
		if p := map[string]int{"timeline": effTimeline, "savers": effSaversPage, "findings": effFindings}[arg]; p > 0 {
			m.setEffPage(p)
		}
		return m.effOpen()
	case "dock":
		var n int
		if _, err := fmt.Sscanf(arg, "%d", &n); err != nil {
			m.flash("how many lines? #dock 6", true)
			return nil
		}
		m.store.Config.DockLines = min(max(n, 1), 15)
		_ = m.store.SaveConfig()
	default:
		m.flash("unknown command #"+name+" · # lists rush's", true)
	}
	return nil
}

func expand(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "~") {
		home, _ := os.UserHomeDir()
		p = home + p[1:]
	}
	if p != "" && !filepath.IsAbs(p) {
		p, _ = filepath.Abs(p)
	}
	return filepath.Clean(p)
}

// relaunch restarts an agent's conversation on account to.
func (m *Model) relaunch(a *fleet.Agent, to agent.Profile) tea.Cmd {
	note := ""
	if to.Name != a.Acct.Name {
		note = "This conversation moved to another account; carry on where you left off."
	}
	mover, ok := agent.As[agent.Mover](agent.Kind(a.Kind))
	if !ok {
		m.flash(agentName(a.Kind)+" can't move a session outside rush mode · /rush moves it over first", true)
		return nil
	}
	mv := agent.Move{From: a.Acct, To: to, Job: a.Job, Extra: a.Extra, Note: note}
	m.flash("relaunching "+a.DisplayName+"…", false)
	return func() tea.Msg {
		id, err := mover.Move(&mv)
		if err != nil {
			return doneMsg{err: err}
		}
		return movedMsg{from: a, to: state.Key(to.Name, id)}
	}
}

func (m *Model) confirmKey(s string) tea.Cmd {
	c := m.confirm
	if c.again != "" && s == c.again {
		s = "y"
	}
	switch s {
	case "y", "enter":
		m.confirm = nil
		if c.onYes != nil {
			return c.onYes()
		}
	case "!":
		m.confirm = nil
		if c.onBang != nil {
			return c.onBang()
		}
	case "n":
		m.confirm = nil
		if c.onNo != nil {
			return c.onNo()
		}
	case "esc", "ctrl+c":
		m.confirm = nil
	}
	return nil
}

func (m *Model) askKillTree(a *fleet.Agent) {
	if a.Worker == nil || m.snap.Table == nil {
		m.flash(a.DisplayName+" has no running process", true)
		return
	}
	memBytes, _, n := m.snap.Table.Sum(a.Worker.PID, nil)
	root := a.Worker.PID
	var start time.Time
	if p := m.snap.Table.Procs[root]; p != nil {
		start = p.Start
	}
	m.confirm = &confirmation{
		question: "Stop " + a.DisplayName + "?",
		detail:   fmt.Sprintf("%d processes · %s · the conversation is kept", n, mem(memBytes)),
		onYes: func() tea.Cmd {
			return cmdErr("stopped "+a.DisplayName, func() error { return stopOutside(a) })
		},
		bangText: "SIGKILL the whole tree",
		onBang:   killTree(root, start),
	}
}

// endOrphans ends each orphaned tree, gently then firmly, and says what it
// freed.
func endOrphans(rows []procRow) tea.Cmd {
	return func() tea.Msg {
		var n int
		var freed uint64
		var errs []string
		for _, r := range rows {
			k, err := actions.EndTree(r.pid, r.start, 3*time.Second)
			if err != nil {
				errs = append(errs, err.Error())
				continue
			}
			n += k
			freed += r.mem
		}
		if n == 0 && len(errs) > 0 {
			return doneMsg{err: errors.New(strings.Join(errs, "; "))}
		}
		return doneMsg{text: fmt.Sprintf("ended %d processes · freed about %s", n, mem(freed))}
	}
}

func killTree(pid int, start time.Time) func() tea.Cmd {
	return func() tea.Cmd {
		return func() tea.Msg {
			n, err := actions.KillTree(pid, start)
			if err != nil {
				return doneMsg{err: err}
			}
			return doneMsg{text: fmt.Sprintf("killed %d processes", n)}
		}
	}
}

func trimCmd(s string, n int) string {
	home, _ := os.UserHomeDir()
	s = strings.ReplaceAll(s, home, "~")
	return ansi.Truncate(s, n, "…")
}
