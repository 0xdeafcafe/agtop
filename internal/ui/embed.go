package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Typing into the live pane: keys go to the session shown on the right while
// the list stays on screen; ctrl+] hands the keyboard back.

func (m *Model) canEmbed() bool {
	a := m.selected()
	return a != nil && m.live != nil && m.live.key == a.Key && m.live.ready.Load() && !m.live.dead.Load()
}

func (m *Model) embedKey(k tea.KeyPressMsg) tea.Cmd {
	if k.String() == "ctrl+]" || !m.canEmbed() || m.zen {
		m.embedded = false
		return nil
	}
	m.sendLive(keyBytes(k))
	return nil
}

func (m *Model) embedPaste(text string) {
	if m.canEmbed() {
		m.sendLive([]byte("\x1b[200~" + text + "\x1b[201~"))
	}
}

func (m *Model) sendLive(b []byte) {
	if len(b) == 0 || m.live == nil || m.live.conn == nil {
		return
	}
	conn := m.live.conn
	go func() { _, _ = conn.Write(b) }()
}

var namedKeys = map[string]string{
	"enter": "\r", "tab": "\t", "shift+tab": "\x1b[Z", "backspace": "\x7f", "esc": "\x1b", "space": " ",
	"up": "\x1b[A", "down": "\x1b[B", "right": "\x1b[C", "left": "\x1b[D",
	"home": "\x1b[H", "end": "\x1b[F", "pgup": "\x1b[5~", "pgdown": "\x1b[6~", "delete": "\x1b[3~",
	"shift+enter": "\x1b\r", "alt+enter": "\x1b\r",
}

// keyBytes turns a key press back into what a terminal would have sent.
func keyBytes(k tea.KeyPressMsg) []byte {
	s := k.String()
	if b, ok := namedKeys[s]; ok {
		return []byte(b)
	}
	if rest, ok := strings.CutPrefix(s, "ctrl+"); ok && len(rest) == 1 && rest[0] >= 'a' && rest[0] <= 'z' {
		return []byte{rest[0] - 'a' + 1}
	}
	if rest, ok := strings.CutPrefix(s, "alt+"); ok && len(rest) == 1 {
		return []byte("\x1b" + rest)
	}
	if k.Text != "" {
		return []byte(k.Text)
	}
	return nil
}
