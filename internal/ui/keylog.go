package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/state"
)

// The key log records the keys agtop receives, and for each enter what it
// did, to find where an enter that seemed to do nothing went. It is on when
// AGTOP_KEYLOG is set or the file keylog exists in agtop's config folder,
// and writes keylog.log beside it. Typed characters are logged as "·", so
// what you write never lands in it.

var keylog struct {
	once sync.Once
	f    *os.File
}

func keylogFile() *os.File {
	keylog.once.Do(func() {
		dir := state.Dir()
		if os.Getenv("AGTOP_KEYLOG") == "" {
			if _, err := os.Stat(filepath.Join(dir, "keylog")); err != nil {
				return
			}
		}
		f, err := os.OpenFile(filepath.Join(dir, "keylog.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err == nil {
			keylog.f = f
		}
	})
	return keylog.f
}

func keylogf(format string, args ...any) {
	f := keylogFile()
	if f == nil {
		return
	}
	_, _ = fmt.Fprintf(f, "%s pid=%d %s\n", time.Now().Format("15:04:05.000"), os.Getpid(), fmt.Sprintf(format, args...))
}

// keyName is a key as the log shows it: a character typed as "·".
func keyName(k tea.KeyPressMsg, s string) string {
	if k.Text != "" && utf8.RuneCountInString(k.Text) == 1 && k.Mod&^tea.ModShift == 0 {
		return "·"
	}
	return fmt.Sprintf("%s(code=%d mod=%d)", s, k.Code, k.Mod)
}

// enterish is a key that sends, or that some terminal might send for
// return: the log follows what each did.
func enterish(s string) bool {
	switch s {
	case "enter", "ctrl+m", "ctrl+j", "alt+enter", "shift+enter", "kpenter":
		return true
	}
	return false
}

// keyState is what an enter is judged by: where the keys go, what is in the
// box, and what is on its way.
func (m *Model) keyState() string {
	var b strings.Builder
	fmt.Fprintf(&b, "mode=%d view=%d solo=%t paneFocus=%t soloAway=%t embedded=%t", m.mode, m.view, m.solo != "", m.paneFocus, m.soloAway, m.embedded)
	fmt.Fprintf(&b, " confirm=%t sheet=%t bar=%t picker=%t dialog=%t", m.confirm != nil, m.sheet != nil, m.bar != nil, m.picker != nil, m.dialog != nil)
	if c := m.host; c == nil {
		fmt.Fprintf(&b, " host=nil opening=%q prompt=%d", m.hostOpening, len(m.input))
	} else {
		fmt.Fprintf(&b, " box=%d back=%d sel=%q card=%q editQ=%d sending=%d queue=%d client=%t state=%q",
			len(c.input), c.back, c.sel, cardKind(c), c.editQ, len(c.sending), len(m.queueOf(c).items), c.client != nil, c.sess.Info.State)
	}
	return b.String()
}

// logKey logs a key as it comes in, and for an enter returns what logs its
// outcome once handled.
func (m *Model) logKey(k tea.KeyPressMsg, s string) func(cmd tea.Cmd) {
	if keylogFile() == nil {
		return nil
	}
	if !enterish(s) {
		keylogf("key %s", keyName(k, s))
		return nil
	}
	before := m.keyState()
	return func(cmd tea.Cmd) {
		keylogf("key %s\n  before %s\n  after  %s cmd=%t", keyName(k, s), before, m.keyState(), cmd != nil)
	}
}

// logInput logs what the terminal sent that can bear on the keys: keys,
// focus, pastes, and input agtop didn't recognise. For an enter it returns
// what logs its outcome.
func (m *Model) logInput(msg tea.Msg) func(cmd tea.Cmd) {
	if keylogFile() == nil {
		return nil
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.logKey(msg, strings.ReplaceAll(msg.String(), "meta+", "super+"))
	case tea.KeyReleaseMsg:
		keylogf("release %s", msg.String())
	case tea.FocusMsg:
		keylogf("focus")
	case tea.BlurMsg:
		keylogf("blur")
	case tea.PasteStartMsg:
		keylogf("paste start")
	case tea.PasteEndMsg:
		keylogf("paste end")
	case tea.PasteMsg:
		keylogf("paste %d bytes", len(msg.Content))
	default:
		if t := fmt.Sprintf("%T", msg); strings.HasPrefix(t, "uv.") {
			keylogf("input %s %q", t, fmt.Sprint(msg))
		}
	}
	return nil
}
