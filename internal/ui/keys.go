package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/actions"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/proc"
	"github.com/0xdeafcafe/agtop/internal/state"
)

func (m *Model) key(k tea.KeyPressMsg) tea.Cmd {
	s := k.String()
	m.hover = "" // the keyboard takes over from the mouse
	if m.host != nil {
		m.host.subHover = ""
	}
	m.lastKeyAt = time.Now()
	if cmd, ok := m.barToggle(s); ok {
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
					m.scanner.Flush()
					return tea.Quit
				},
			}
			return nil
		}
		m.scanner.Flush()
		return tea.Quit
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
		return m.pickerKey(s)
	}
	// , and . (or < and >) with nothing typed go to the previous and next
	// place (ctrl+\
	// still goes to the next), ctrl+z turns Zen on and off, from anywhere
	// but a question being asked.
	if d := m.placeStep(s); d != 0 {
		m.setView(m.view + d)
		return m.loadPreview()
	}
	if s == "ctrl+z" && m.mode != modeCwd && (m.dialog == nil || m.dialog.asking == "") {
		m.setZen(!m.zen)
		return m.loadPreview()
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
			m.input, m.back, m.anchor = m.input[:0], 0, 0
			return nil
		}
		return m.quitKey()
	}
	// On a Claude Code agent's screen, typing goes into it: ← moves its
	// cursor rather than leaving. tab, [ ] and ctrl+] stay agtop's.
	if !m.zen && m.paneFocus && m.host != nil && m.mode == modeList && m.dialog == nil && m.viewName(m.host) == "screen" && m.canEmbed() {
		switch s {
		case "tab", "shift+tab", "[", "]", "ctrl+]":
		default:
			m.embedded = true
			return m.embedKey(k)
		}
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
	// In Machine and Settings tab goes through the place's pages.
	if (s == "tab" || s == "shift+tab") && (m.mode == modeProcs || m.mode == modeCleanup || (m.dialog != nil && m.dialog.asking == "")) {
		d := 1
		if s == "shift+tab" {
			d = -1
		}
		if m.dialog != nil {
			m.setSettingsPage(m.dialog.tab + d)
		} else {
			m.setMachinePage(m.machinePage + d)
		}
		return nil
	}
	if m.dialog != nil {
		return m.dialogKey(k, s)
	}
	switch m.mode {
	case modeHelp:
		switch s {
		case "tab", "right", "l":
			m.helpPage = (m.helpPage + 1) % len(helpPages)
		case "shift+tab", "left", "h":
			m.helpPage = (m.helpPage + len(helpPages) - 1) % len(helpPages)
		case "1", "2", "3":
			m.helpPage = int(s[0] - '1')
		default:
			m.mode = modeList
		}
		return nil
	case modeProcs:
		return m.procKey(s)
	case modeCleanup:
		return m.cleanupKey(s)
	case modeCwd:
		return m.cwdKey(k, s)
	}
	return m.listKey(k, s)
}

func (m *Model) editKey(k tea.KeyPressMsg, s string) bool {
	switch s {
	case "backspace", "ctrl+h":
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
	case "ctrl+u":
		m.input = m.input[:0]
	case "ctrl+w", "alt+backspace":
		t := strings.TrimRight(string(m.input), " ")
		if i := strings.LastIndexByte(t, ' '); i >= 0 {
			m.input = []rune(t[:i+1])
		} else {
			m.input = m.input[:0]
		}
	default:
		if k.Text != "" && k.Mod&^tea.ModShift == 0 {
			m.input = append(m.input, []rune(k.Text)...)
			return true
		}
		return false
	}
	return true
}

// placeStep is which way a key moves between places: -1 for , and <, +1
// for . > and ctrl+\, 0 when it doesn't. , and . move without shift; they
// and < > only move with nothing typed in the box that has the keys, so
// they can still be typed.
func (m *Model) placeStep(s string) int {
	d := map[string]int{",": -1, "<": -1, ".": 1, ">": 1, "ctrl+\\": 1}[s]
	if d == 0 || m.mode == modeCwd || m.dialog != nil && m.dialog.asking != "" {
		return 0
	}
	if s == "ctrl+\\" || m.mode != modeList || m.dialog != nil {
		return d
	}
	if c := m.host; m.paneFocus && c != nil {
		// A Claude Code agent's screen takes what's typed itself.
		if m.viewName(c) == "screen" && m.canEmbed() || len(c.input) > 0 || c.editQ > 0 {
			return 0
		}
		return d
	}
	if len(m.input) > 0 || m.inKind != inPrompt {
		return 0
	}
	return d
}

// switchFocus is tab in Agents: from the list into the selected agent's
// Session, and back. In Zen the list is only the agents waiting on you.
func (m *Model) switchFocus() tea.Cmd {
	if m.paneFocus && m.host != nil {
		if m.zen {
			m.paneFocus, m.preview, m.zenList = false, true, true // the waiting list beside it
			return nil
		}
		m.leavePane()
		return nil
	}
	if m.zen && m.zenList {
		m.zenList, m.paneFocus = false, true
		return m.loadPreview()
	}
	a := m.selected()
	if a == nil || strings.HasPrefix(m.sel, "§") {
		return nil
	}
	if a.Agtop {
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
	a := m.selected()
	empty := len(m.input) == 0
	if (s == "backspace" || s == "ctrl+h") && empty && len(m.images) > 0 {
		m.images = m.images[:len(m.images)-1]
		return nil
	}
	if s == "ctrl+v" && m.acceptsText() && m.dialog == nil {
		return pasteClipImage()
	}
	if cmd, used := m.fleetSlashKey(s); used {
		return cmd
	}
	if s == "enter" && empty && len(m.images) > 0 && m.inKind == inPrompt {
		return m.startHosted("", m.startDir())
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
	case "pgdown", "ctrl+d":
		m.move(10)
		return m.loadPreview()
	case "home":
		if empty {
			m.move(-len(m.order))
			return m.loadPreview()
		}
	case "end":
		if empty {
			m.move(len(m.order))
			return m.loadPreview()
		}
	case "right":
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
		// In Agents, ← has nothing to go back to; sections fold with enter.
		if empty {
			return nil
		}
	case "esc":
		switch {
		case !empty:
			m.input = m.input[:0]
			if m.inKind != inReply {
				m.inKind = inPrompt
			}
		case m.inKind != inPrompt:
			m.inKind = inPrompt
		case m.preview:
			m.preview, m.full = false, false
		case m.armed != "":
			m.armed = ""
		case time.Since(m.quitArmed) < 2*time.Second:
			m.scanner.Flush()
			return tea.Quit
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
		// Enter on an agent renames it, as in the Finder, or opens it, as
		// you chose the first time; ⌘↓, → and tab always open it.
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
			m.flash("finish or clear the draft first (esc)", true)
		default:
			m.startRename(a)
		}
		return nil
	case "super+down":
		// ⌘↓ opens, as in the Finder: into the agent's Session message box.
		if empty && a != nil && !strings.HasPrefix(m.sel, "§") {
			return m.focusPane(a)
		}
		return nil
	case "ctrl+l":
		// Drafting a new session, or nothing selected: choose where it
		// starts. Otherwise: move the selected agent.
		drafting := !empty && m.inKind == inPrompt && !isHashCmd(string(m.input))
		if drafting || a == nil {
			m.openDirPicker()
			return nil
		}
		m.openCwd(a)
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
	case "ctrl+o":
		// Reply: straight into the agent's Session message box.
		if a != nil {
			return m.focusPane(a)
		}
		return nil
	case "ctrl+x":
		return m.stopOrRemove(a)
	case "alt+d":
		// Done with it: to Done, its idle process stopped.
		return m.markDone(a)
	case "shift+up", "shift+down":
		n := m.dockLines()
		if s == "shift+up" {
			n++
		} else {
			n--
		}
		m.store.Config.DockLines = min(max(n, 1), 15)
		_ = m.store.SaveConfig()
		return nil
	case "[", "]":
		if empty && a != nil {
			if c := m.host; c != nil && c.key == a.Key {
				n, d := len(m.views(c)), 1
				if s == "[" {
					d = -1
				}
				c.view, c.scroll = (c.view%n+d+n)%n, 0
			} else {
				m.claudeView = 1 - m.claudeView
			}
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
	}
	if s == "ctrl+g" {
		return editDraft(&m.pastes, m.input, false)
	}
	if (s == "backspace" || s == "ctrl+h") && m.anchor == 0 {
		if buf, pos, ok := dropChip(m.input, m.cursorPos()); ok {
			m.input = buf
			m.setCursor(pos)
			return nil
		}
	}
	before := string(m.input)
	m.editInput(k, s)
	if s == "space" && (m.inKind == inPrompt || m.inKind == inReply) && m.anchor == 0 {
		// A path to an image, typed or dropped in as keys, becomes an
		// attachment once it's done.
		if in, imgs := pullImages(m.input, m.images); len(imgs) > len(m.images) {
			m.input, m.images = in, imgs
			m.setCursor(len(in))
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
		m.sel, m.armed = items[j], ""
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

// quitKey arms quitting on the first ctrl+c and quits on a second one.
func (m *Model) quitKey() tea.Cmd {
	if time.Since(m.quitArmed) < 2*time.Second {
		m.scanner.Flush()
		return tea.Quit
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
	next := groupModes[0]
	for i, g := range groupModes {
		if g == cur {
			next = groupModes[(i+1)%len(groupModes)]
		}
	}
	m.store.Config.GroupBy = next
	_ = m.store.SaveConfig()
	m.flash("grouped by "+next, false)
	m.rebuild()
}

func (m *Model) stopOrRemove(a *fleet.Agent) tea.Cmd {
	if a == nil {
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
		return cmdErr("stopped "+a.DisplayName, func() error { return actions.Stop(a.Acct, a.ID, a.PID) })
	}
	if m.armed == a.Key && time.Since(m.armedAt) < 5*time.Second {
		m.armed = ""
		m.flash("deleting "+a.DisplayName+"…", false)
		return cmdErr("deleted "+a.DisplayName, func() error { return actions.Remove(a.Acct, a.ID) })
	}
	m.armed, m.armedAt = a.Key, time.Now()
	m.flash("ctrl+x again within 5s to delete "+a.DisplayName+" (and its worktree, when that's safe)", false)
	return nil
}

func (m *Model) submit() tea.Cmd {
	text := strings.TrimSpace(m.pastes.expand(string(m.input), false))
	tagged := strings.TrimSpace(m.pastes.expand(string(m.input), true)) // for agtop sessions
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
	m.pastes = pastes{}
	m.input, m.inKind = m.input[:0], inPrompt
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
		m.markSeen(a)
		m.flash("sending to "+a.DisplayName+"…", false)
		m.loader.Nudge(a.Key)
		m.refresh()
		if a.Agtop {
			return sendHosted(a, tagged)
		}
		text = withImages(text, m.images)
		m.images = nil
		if q := m.localQ[a.Key]; busy(a) || q != nil && len(q.items) > 0 {
			m.queueLocal(a.Key, text)
			m.flash(fmt.Sprintf("queued for %s · goes within 15s", a.DisplayName), false)
			return nil
		}
		return reply(a, text)
	}
	if text == "" {
		return m.attach(a)
	}
	if isHashCmd(text) {
		return m.command(a, text)
	}
	if !strings.HasPrefix(text, "/") {
		m.didStep("start")
	}
	if strings.HasPrefix(text, "/") {
		if cmd, ok := m.legacyCommand(text); ok {
			return cmd
		}
	}
	// The Prompt only starts new sessions; replies go through a Session's
	// own message box.
	if d := m.store.Config.Dispatch; d.RunIn != "daemon" && (d.Agent == "" || d.Agent == "claude") {
		return m.startHosted(tagged, m.startDir())
	}
	acct := m.store.Config.ActiveAccount()
	dir := m.startDir()
	flags := m.store.Config.Dispatch.Flags()
	m.flash("starting a new session…", false)
	return func() tea.Msg {
		id, err := actions.Dispatch(acct, dir, text, flags...)
		if err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{text: "started " + id}
	}
}

// command runs one of agtop's # commands on agent a: the selected one from
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
	need := func() bool {
		if a == nil {
			m.flash("select an agent first", true)
			return false
		}
		return true
	}
	m.didStep("hash")
	switch name {
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
	case "clean":
		if strings.TrimSpace(arg) == "all" {
			m.askCleanAll()
		} else if need() {
			m.askClean(a)
		}
	case "stop":
		if need() {
			return cmdErr("stopped "+a.DisplayName, func() error { return actions.Stop(a.Acct, a.ID, a.PID) })
		}
	case "rm":
		if need() {
			m.confirm = &confirmation{
				question: "Delete " + a.DisplayName + "?",
				detail:   "removes the session, and its worktree when that's safe",
				onYes: func() tea.Cmd {
					return cmdErr("deleted "+a.DisplayName, func() error { return actions.Remove(a.Acct, a.ID) })
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
	case "cd":
		if need() {
			if expand(arg) == "" {
				m.flash("which folder? /cd <path>", true)
				return nil
			}
			return m.relaunch(a, expand(arg), nil, a.Acct)
		}
	case "add-dir":
		if need() {
			if expand(arg) == "" {
				m.flash("which folder? /add-dir <path>", true)
				return nil
			}
			return m.relaunch(a, "", []string{expand(arg)}, a.Acct)
		}
	case "account":
		if arg == "" {
			m.setView(2)
			m.setSettingsPage(tabAccounts)
			return nil
		}
		return m.useLogin(arg)
	case "group":
		if need() {
			m.inKind, m.input, m.promptFor = inGroup, []rune(arg), a.Key
			return m.submit()
		}
	case "by":
		for _, g := range groupModes {
			if g == arg {
				m.store.Config.GroupBy = g
				_ = m.store.SaveConfig()
				m.rebuild()
				return nil
			}
		}
		m.flash("group by one of: "+strings.Join(groupModes, ", "), true)
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
			m.flash("list width back to agtop's choice · /width 30% sets your own", false)
			return nil
		}
		m.setSideWidth(int(pct / 100 * float64(m.w)))
	case "agtop":
		if need() {
			return m.moveToAgtop(a)
		}
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
	case "help":
		m.mode = modeHelp
		m.didStep("keys")
	case "quit":
		m.scanner.Flush()
		return tea.Quit
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
			if a.Agtop || a.Interactive {
				m.flash("only a Claude Code agent in the background opens full screen", true)
				return nil
			}
			return m.attach(a)
		}
	case "folder":
		m.openDirPicker()
	case "statusline":
		m.openTopBar(a)
	case "dock":
		var n int
		if _, err := fmt.Sscanf(arg, "%d", &n); err != nil {
			m.flash("how many lines? #dock 6", true)
			return nil
		}
		m.store.Config.DockLines = min(max(n, 1), 15)
		_ = m.store.SaveConfig()
	default:
		m.flash("unknown command #"+name+" · # lists agtop's", true)
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

func (m *Model) relaunch(a *fleet.Agent, dir string, addDirs []string, to claude.Account) tea.Cmd {
	note := ""
	if dir != "" && dir != a.Cwd {
		note = fmt.Sprintf("Your working directory is now %s (it was %s). Paths from earlier in this conversation point at the old folder.", dir, a.Cwd)
	}
	if len(addDirs) > 0 {
		note = fmt.Sprintf("You now also have access to %s.", strings.Join(addDirs, ", "))
	}
	if to.Name != a.Acct.Name && note == "" {
		note = "This conversation moved to another account; carry on where you left off."
	}
	r := actions.Relaunch{From: a.Acct, To: to, Job: a.Job, Dir: dir, AddDirs: addDirs, Note: note}
	m.flash("relaunching "+a.DisplayName+"…", false)
	return func() tea.Msg {
		id, err := r.Run()
		if err != nil {
			return doneMsg{err: err}
		}
		return movedMsg{from: a, to: state.Key(to.Name, id)}
	}
}

func (m *Model) confirmKey(s string) tea.Cmd {
	c := m.confirm
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
			return cmdErr("stopped "+a.DisplayName, func() error { return actions.Stop(a.Acct, a.ID, a.PID) })
		},
		bangText: "SIGKILL the whole tree",
		onBang:   killTree(root, start),
	}
}

// procRows are the rows the process screen shows, in order: orphans, then
// every agent busiest first with what it is running under it, then the
// Claude processes that belong to no agent.
func (m *Model) procRows() []procRow {
	tab := m.snap.Table
	if tab == nil {
		return nil
	}
	var out []procRow
	// Orphans first: nothing else will ever stop them, so they wait on you.
	// Under each, the few processes holding most of its memory.
	for _, r := range m.snap.Machine.Rows {
		if r.Role != fleet.RoleOrphan {
			continue
		}
		out = append(out, procRow{pid: r.PID, label: r.Label, cmd: r.Cmd, mem: r.Mem, cpu: r.CPU, n: r.Procs, start: r.Start, role: r.Role, other: true})
		kids := tab.Tree(r.PID)[1:]
		sort.SliceStable(kids, func(i, j int) bool { return kids[i].Footprint > kids[j].Footprint })
		for _, n := range kids[:min(3, len(kids))] {
			if n.Footprint >= 32<<20 {
				out = append(out, procRow{pid: n.PID, depth: 1, cmd: m.shortCmd(n.PID, n.Comm), mem: n.Footprint, cpu: n.CPU, n: 1, start: n.Start, role: fleet.RoleOrphan})
			}
		}
	}
	type agentRows struct {
		a     *fleet.Agent
		tools []procRow
	}
	var agents []agentRows
	for _, a := range m.snap.Agents {
		if a.PID == 0 || tab.Procs[a.PID] == nil {
			continue
		}
		agents = append(agents, agentRows{a, m.toolRows(a)})
	}
	// Agents running something come first, busiest first; the idle rest
	// keep still, alphabetically, so the list doesn't shuffle under you.
	busy := func(x agentRows) bool { return len(x.tools) > 0 || x.a.CPU >= 5 }
	sort.SliceStable(agents, func(i, j int) bool {
		if bi, bj := busy(agents[i]), busy(agents[j]); bi != bj {
			return bi
		} else if bi && agents[i].a.CPU != agents[j].a.CPU {
			return agents[i].a.CPU > agents[j].a.CPU
		}
		return strings.ToLower(agents[i].a.DisplayName) < strings.ToLower(agents[j].a.DisplayName)
	})
	for _, x := range agents {
		a := x.a
		out = append(out, procRow{pid: a.PID, label: oneLine(a.DisplayName), cmd: m.context(a), mem: a.Mem, cpu: a.CPU,
			n: a.Procs, start: tab.Procs[a.PID].Start, key: a.Key, heading: true, busy: busy(x)})
		out = append(out, x.tools...)
	}
	for _, r := range m.snap.Machine.Rows {
		if r.Role == fleet.RoleWorker || r.Role == fleet.RoleOrphan {
			continue
		}
		out = append(out, procRow{pid: r.PID, label: r.Label, cmd: r.Cmd, mem: r.Mem, cpu: r.CPU, n: r.Procs, start: r.Start, role: r.Role, other: true})
	}
	return out
}

// toolRows are what an agent is running: its process tree without the
// agent's own processes (agtop's host, Claude Code, its pty host), which its
// heading row stands for, and with each Bash-tool shell shown as the command
// it was asked to run rather than the snapshot-sourcing wrapper around it.
func (m *Model) toolRows(a *fleet.Agent) []procRow {
	tab := m.snap.Table
	var out []procRow
	depth := map[int]int{}    // the depth a process's children are drawn at
	shown := map[int]string{} // what a drawn process was drawn as
	for _, n := range tab.Tree(a.PID) {
		d := depth[n.PPID]
		if n.PID == a.PID {
			d = 0
		}
		depth[n.PID] = d
		if own(n.Comm) {
			continue
		}
		cmd := m.shortCmd(n.PID, n.Comm)
		if full := strings.Join(proc.Args(n.PID), " "); strings.Contains(full, "eval '") {
			if c := fleet.ShellCmd(full); c != full {
				cmd = "$ " + trimCmd(oneLine(c), 200)
			}
		}
		// A shell's only job is often the one thing it was asked to run.
		if p, ok := shown[n.PPID]; ok && (p == cmd || p == "$ "+cmd) {
			shown[n.PID] = cmd
			continue
		}
		shown[n.PID] = cmd
		depth[n.PID] = d + 1
		out = append(out, procRow{pid: n.PID, depth: d, cmd: cmd, mem: n.Footprint, cpu: n.CPU, n: 1, start: n.Start, key: a.Key})
	}
	return out
}

// own reports whether a process is part of the agent itself rather than
// something it runs.
func own(comm string) bool {
	switch filepath.Base(comm) {
	case "claude", "agtop":
		return true
	}
	return false
}

// procIndex is where the cursor is: on the process it was on, wherever the
// list has moved it.
func (m *Model) procIndex(rows []procRow) int {
	if m.procPID != 0 {
		for i, r := range rows {
			if r.pid == m.procPID {
				return i
			}
		}
	}
	return max(0, min(m.procCursor, len(rows)-1))
}

type procRow struct {
	pid, depth, n int
	label, cmd    string
	mem           uint64
	cpu           float64
	start         time.Time
	role          fleet.Role
	key           string
	heading       bool // an agent's own row above its tree
	busy          bool // a heading whose agent is running something
	other         bool // a Claude process that belongs to no agent
}

func (m *Model) procKey(s string) tea.Cmd {
	rows := m.procRows()
	m.procCursor = m.procIndex(rows)
	defer func() {
		if m.procCursor < len(rows) {
			m.procPID = rows[m.procCursor].pid
		}
	}()
	switch s {
	case "esc", "q", "ctrl+p", "left":
		m.setView(0)
	case "up", "k":
		m.procCursor = roundMove(m.procCursor, -1, len(rows))
	case "down", "j":
		m.procCursor = roundMove(m.procCursor, 1, len(rows))
	case "enter":
		if m.procCursor < len(rows) && rows[m.procCursor].key != "" {
			m.sel = rows[m.procCursor].key
			m.setView(0)
		}
	case "X":
		if mc := m.snap.Machine; mc.Orphans > 0 {
			var ends []procRow
			for _, r := range rows {
				if r.role == fleet.RoleOrphan && r.other {
					ends = append(ends, r)
				}
			}
			m.confirm = &confirmation{
				question: fmt.Sprintf("End all %d orphaned process trees and free about %s?", len(ends), mem(mc.OrphanMem)),
				detail:   "SIGTERM, then SIGKILL after 3s",
				onYes:    func() tea.Cmd { return endOrphans(ends) },
			}
		}
	case "ctrl+x", "x":
		if m.procCursor < len(rows) && rows[m.procCursor].role == fleet.RoleOrphan && rows[m.procCursor].other {
			r := rows[m.procCursor]
			m.confirm = &confirmation{
				question: fmt.Sprintf("End %s and everything under it, freeing about %s?", trimCmd(r.cmd, 40), mem(r.mem)),
				detail:   "SIGTERM, then SIGKILL after 3s",
				onYes:    func() tea.Cmd { return endOrphans([]procRow{r}) },
				bangText: "SIGKILL it now",
				onBang:   killTree(r.pid, r.start),
			}
		} else if m.procCursor < len(rows) {
			r := rows[m.procCursor]
			m.confirm = &confirmation{
				question: fmt.Sprintf("Send SIGTERM to %d?", r.pid),
				detail:   trimCmd(r.cmd, 80),
				onYes: func() tea.Cmd {
					return cmdErr(fmt.Sprintf("sent SIGTERM to %d", r.pid), func() error { return actions.Terminate(r.pid) })
				},
				bangText: "SIGKILL it and everything under it",
				onBang:   killTree(r.pid, r.start),
			}
		}
	case "!":
		if m.procCursor < len(rows) {
			r := rows[m.procCursor]
			m.confirm = &confirmation{
				question: fmt.Sprintf("SIGKILL %d and everything under it?", r.pid),
				detail:   trimCmd(r.cmd, 80),
				onYes:    killTree(r.pid, r.start),
			}
		}
	}
	return nil
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

func (m *Model) openCwd(a *fleet.Agent) {
	m.mode, m.cwdFor, m.cwdCursor, m.cwdMove = modeCwd, a.Key, -1, true
	m.input = []rune(tildify(a.Cwd))
}

// cwdChoices are the folders worth offering: every folder an agent ran in,
// their repositories, and those repositories' Claude worktrees.
func (m *Model) cwdChoices() []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, a := range m.snap.Agents {
		add(a.Repo)
		if a.Repo != "" {
			wt := filepath.Join(a.Repo, ".claude", "worktrees")
			if ents, err := os.ReadDir(wt); err == nil {
				for _, e := range ents {
					if e.IsDir() {
						add(filepath.Join(wt, e.Name()))
					}
				}
			}
		}
		add(a.Cwd)
	}
	q := strings.ToLower(strings.TrimSpace(string(m.input)))
	q = strings.TrimPrefix(q, "~")
	var hits []string
	for _, p := range out {
		if q == "" || strings.Contains(strings.ToLower(p), q) {
			hits = append(hits, p)
		}
	}
	sort.Strings(hits)
	return hits
}

func (m *Model) cwdKey(k tea.KeyPressMsg, s string) tea.Cmd {
	choices := m.cwdChoices()
	switch s {
	case "esc":
		m.mode, m.input = modeList, m.input[:0]
	case "tab":
		m.cwdMove = !m.cwdMove
	case "up", "down":
		// -1 is the typed path, before the first choice.
		d := map[string]int{"up": -1, "down": 1}[s]
		m.cwdCursor = roundMove(m.cwdCursor+1, d, len(choices)+1) - 1
	case "enter":
		target := expand(string(m.input))
		if m.cwdCursor >= 0 && m.cwdCursor < len(choices) {
			target = choices[m.cwdCursor]
		}
		var a *fleet.Agent
		for _, x := range m.snap.Agents {
			if x.Key == m.cwdFor {
				a = x
			}
		}
		m.mode, m.input = modeList, m.input[:0]
		if a == nil {
			return nil
		}
		if m.cwdMove {
			return m.relaunch(a, target, nil, a.Acct)
		}
		return m.relaunch(a, "", []string{target}, a.Acct)
	default:
		if m.editKey(k, s) {
			m.cwdCursor = -1
		}
	}
	return nil
}
