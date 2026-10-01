package ui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/keymap"
)

// keyState is the keymap in force and a chord being typed.
type keyState struct {
	m    *keymap.Map
	file keymap.File
	// chord is the keys of a chord typed so far, and when the last came.
	chord   keymap.Seq
	chordAt time.Time
	// capture hands the next key to Settings, unmapped, to bind it.
	capture func(s string) tea.Cmd
	// page is Settings, Keys: keys being taken for an action.
	page keysPage
}

// chordWait is how long a chord waits for its next key.
const chordWait = 2 * time.Second

// keyActions are every action a key can be bound to: rush's keys, its #
// commands, and what plugins offer.
func (m *Model) keyActions() []keymap.Action {
	out := append([]keymap.Action(nil), keymap.Defaults...)
	for _, c := range fleetCommands {
		out = append(out, keymap.Action{ID: keymap.CommandID(c.Name), Context: keymap.Any, Title: "#" + c.Name + ": " + c.Description})
	}
	out = append(out, m.pluginActions()...)
	return out
}

// loadKeys reads keybindings.json, off the UI, and puts it in force.
func (m *Model) loadKeys() tea.Cmd {
	return sheetDo(keymap.Load, func(m *Model, f keymap.File, err error) tea.Cmd {
		if err != nil {
			m.flash("keybindings.json: "+err.Error(), true)
		}
		m.setKeys(f)
		return nil
	})
}

// setKeys puts f in force.
func (m *Model) setKeys(f keymap.File) {
	m.keys.file = f
	m.keys.m = keymap.Build(m.keyActions(), f, m.pluginKeys())
}

// keyMap is the keymap in force; before keybindings.json is read, rush's
// own.
func (m *Model) keyMap() *keymap.Map {
	if m.keys.m == nil {
		m.keys.m = keymap.Build(m.keyActions(), keymap.File{}, nil)
	}
	return m.keys.m
}

// keyContexts is where a key lands now, beyond Global: nothing more while
// a sheet, the command bar or a question has the keys.
func (m *Model) keyContexts() []keymap.Context {
	if m.onPages() {
		return []keymap.Context{keymap.Pages}
	}
	if m.bar != nil || m.confirm != nil || m.sheet != nil || m.picker != nil || m.dialog != nil ||
		m.embedded || m.mode != modeList || m.editingDoc() {
		return nil
	}
	if m.paneFocus && m.host != nil {
		return []keymap.Context{keymap.Session}
	}
	if m.inKind == inPrompt && len(m.input) > 0 {
		return []keymap.Context{keymap.Prompt, keymap.List}
	}
	return []keymap.Context{keymap.List}
}

// onPages is whether a place's pages have the keys: Efficiency, Settings,
// Projects or the Wall, with nothing over them and nothing being typed.
func (m *Model) onPages() bool {
	if m.bar != nil || m.confirm != nil || m.sheet != nil || m.picker != nil || m.embedded || m.editingDoc() {
		return false
	}
	if d := m.dialog; d != nil {
		return m.view == placeSettings && d.asking == ""
	}
	switch m.mode {
	case modeEff:
		return !m.eff.typing() && m.eff.plan == nil
	case modeWall:
		return true
	}
	return false
}

// remapKey turns the key pressed into the one rush's handling expects, by
// the keymap. It reports true when the key is used up: a chord begun, a
// command run, or a key that no longer does anything.
func (m *Model) remapKey(k *tea.KeyPressMsg, s *string) (tea.Cmd, bool) {
	if f := m.keys.capture; f != nil {
		m.keys.capture = nil
		return f(*s), true
	}
	if m.embedded && *s == "ctrl+]" {
		return nil, false // always hands Claude Code's screen back, never a chord
	}
	if len(m.keys.chord) > 0 && time.Since(m.keys.chordAt) > chordWait {
		m.keys.chord = nil
	}
	r := m.keyMap().Resolve(m.keyContexts(), m.keys.chord, *s)
	m.keys.chord = nil
	switch {
	case len(r.Pending) > 0:
		m.keys.chord, m.keys.chordAt = r.Pending, time.Now()
		m.flash(r.Pending.String()+" …", false)
		return nil, true
	case len(r.Missed) > 0:
		m.flash(r.Missed.String()+" does nothing", true)
		return nil, true
	case r.Run != "":
		return m.runAction(r.Run), true
	case r.Key == "":
		return nil, true
	case r.Key != *s:
		nk, ok := keyOf(r.Key)
		if !ok {
			return nil, false
		}
		*k, *s = nk, r.Key
	}
	return nil, false
}

// runAction runs a command action: a # command on the agent in view, a
// plugin's command, or one of the Prompt's.
func (m *Model) runAction(id string) tea.Cmd {
	a := m.selected()
	if m.paneFocus && m.host != nil {
		a = m.focused()
	}
	if name, ok := strings.CutPrefix(id, "command:"); ok {
		return m.command(a, "#"+name)
	}
	if rest, ok := strings.CutPrefix(id, "plugin:"); ok {
		return m.runPluginCommand(rest, a)
	}
	switch id {
	case "session.history.open", "session.history.close":
		m.setHistoryFold(id == "session.history.open")
		return nil
	case "session.setup":
		if m.host != nil {
			return m.openSwitchSheet(m.host)
		}
		return nil
	case "session.mode":
		return m.cycleSessionPermission()
	case "guide.open":
		m.helpPage = 1
		if m.paneFocus && m.host != nil {
			m.helpPage = 3
		}
		m.mode = modeHelp
		return nil
	case "prompt.stash":
		return m.stashCommand("stash")
	case "prompt.history":
		return m.stashCommand("history")
	}
	return nil
}

func (m *Model) cycleSessionPermission() tea.Cmd {
	return m.permissionCommand(m.host, "", false)
}
