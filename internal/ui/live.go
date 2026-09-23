package ui

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"

	"github.com/0xdeafcafe/agtop/internal/daemon"
	"github.com/0xdeafcafe/agtop/internal/fleet"
)

// live is an agent's own terminal, attached in the background the same way
// enter attaches, and replayed into an emulator the size of the preview pane.
// The preview then shows exactly what the session draws instead of a summary
// rebuilt from its transcript.
type live struct {
	key   string
	short string
	cl    daemon.Client
	id    string
	conn  net.Conn

	mu     sync.Mutex // guards emu and cursor
	emu    *vt.Emulator
	cursor bool

	w, h  int
	ready atomic.Bool
	dead  atomic.Bool
	wake  chan struct{}
}

// frameEvery caps how often a busy session repaints the pane.
const frameEvery = 33 * time.Millisecond

type liveOpenMsg struct {
	l   *live
	err error
}

type liveMsg struct{ l *live }

// liveCapable is true for sessions the daemon hosts with a process running.
// Attaching to anything else would respawn it just to show a preview.
func liveCapable(a *fleet.Agent) bool {
	return a != nil && !a.Interactive && a.Worker != nil && (daemon.Client{Account: a.Acct}).Running()
}

func openLive(a *fleet.Agent, w, h int) tea.Cmd {
	cl := daemon.Client{Account: a.Acct}
	l := &live{
		key: a.Key, short: a.ID, cl: cl, w: w, h: h,
		id:   fmt.Sprintf("agtop-preview-%d", os.Getpid()),
		wake: make(chan struct{}, 1),
		emu:  vt.NewEmulator(w, h),
	}
	l.emu.SetScrollbackSize(0) // only the visible screen is ever drawn
	l.emu.SetCallbacks(vt.Callbacks{CursorVisibility: func(v bool) { l.cursor = v }})
	return func() tea.Msg {
		conn, r, info, err := cl.Attach(l.short, l.id, w, h)
		if err != nil {
			return liveOpenMsg{l: l, err: err}
		}
		l.conn = conn
		var modes strings.Builder
		for _, m := range info.DecModes {
			fmt.Fprintf(&modes, "\x1b[?%dh", m)
		}
		_, _ = l.write([]byte(modes.String()))
		// Answers to the session's terminal queries go back up the stream.
		go func() { _, _ = io.Copy(conn, l.emu) }()
		go func() {
			_ = daemon.Relay(r, writerFunc(func(p []byte) (int, error) {
				n, err := l.write(p)
				l.ready.Store(true)
				l.poke()
				return n, err
			}))
			l.dead.Store(true)
			l.poke()
		}()
		return liveOpenMsg{l: l}
	}
}

func (l *live) write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.emu.Write(p)
}

func (l *live) poke() {
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

// next waits for new output, then lets a burst settle so a busy session
// costs a frame every frameEvery rather than one per write.
func (l *live) next() tea.Cmd {
	return func() tea.Msg {
		<-l.wake
		if !l.dead.Load() {
			time.Sleep(frameEvery)
		}
		return liveMsg{l}
	}
}

func (l *live) resize(w, h int) {
	if w == l.w && h == l.h {
		return
	}
	l.w, l.h = w, h
	l.mu.Lock()
	l.emu.Resize(w, h)
	l.mu.Unlock()
	go func() { _ = l.cl.Resize(l.short, l.id, w, h) }()
}

func (l *live) close() {
	l.dead.Store(true)
	if l.conn != nil {
		_ = l.conn.Close()
	}
	_ = l.emu.Close()
}

// lines renders the emulator screen, with the session's cursor drawn in
// reverse video when it shows one.
func (l *live) lines() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	pos := l.emu.CursorPosition()
	var restore *uv.Cell
	if l.cursor {
		if c := l.emu.CellAt(pos.X, pos.Y); c != nil {
			restore = c.Clone()
			rc := c.Clone()
			if rc.Content == "" {
				rc.Content, rc.Width = " ", 1
			}
			rc.Style.Attrs |= uv.AttrReverse
			l.emu.SetCell(pos.X, pos.Y, rc)
		}
	}
	out := strings.Split(l.emu.Render(), "\n")
	if restore != nil {
		l.emu.SetCell(pos.X, pos.Y, restore)
	}
	return out
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// syncLive keeps one background attach open for the agent in the preview,
// sized to the pane, and drops it when the preview closes or moves on.
func (m *Model) syncLive() tea.Cmd {
	a := m.selected()
	w, h := m.liveSize()
	showing := (m.preview || m.wide()) && m.mode == modeList && m.dialog == nil
	// While the user is inside a session (enter), its attach is the only one.
	if m.attached != "" || !showing || !liveCapable(a) || w < 20 || h < 4 {
		m.closeLive()
		return nil
	}
	if m.liveFailed == a.Key && time.Since(m.liveFailedAt) < 10*time.Second {
		return nil
	}
	if m.live != nil && m.live.key == a.Key {
		m.live.resize(w, h)
		return nil
	}
	if m.liveOpening == a.Key {
		return nil
	}
	m.closeLive()
	m.liveOpening = a.Key
	return openLive(a, w, h)
}

func (m *Model) closeLive() {
	if m.live != nil {
		m.live.close()
		m.live = nil
	}
	m.liveOpening = ""
}

func (m *Model) onLiveOpen(msg liveOpenMsg) tea.Cmd {
	if msg.l.key != m.liveOpening {
		if msg.err == nil {
			msg.l.close()
		}
		return nil
	}
	m.liveOpening = ""
	if msg.err != nil {
		// The transcript preview stays up; say why the live one is missing.
		m.flash("live view unavailable: "+msg.err.Error(), true)
		m.liveFailed, m.liveFailedAt = msg.l.key, time.Now()
		return nil
	}
	m.live = msg.l
	return m.live.next()
}

func (m *Model) onLive(msg liveMsg) tea.Cmd {
	if msg.l != m.live {
		return nil
	}
	if m.live.dead.Load() {
		// The stream ended: release it and wait before trying again, so a
		// session opened elsewhere is not fought over.
		m.liveFailed, m.liveFailedAt = m.live.key, time.Now()
		m.live.close()
		m.live = nil
		return nil
	}
	return m.live.next()
}

// liveSize is the emulator size for the current pane: the pane less its
// one-line title.
func (m *Model) liveSize() (int, int) {
	_, paneW, _ := m.layout()
	return paneW - 3, m.paneH() - 1
}

// liveLines is the pane when the focused agent's terminal is showing, or nil
// to fall back to the transcript preview.
func (m *Model) liveLines(w int) []string {
	a := m.selected()
	l := m.live
	if a == nil || l == nil || l.key != a.Key || !l.ready.Load() {
		return nil
	}
	title := faint("▍") + paint(cText+bold, oneLine(a.DisplayName)) + "  " + paint(cOrange, "●") + dim(" live")
	if m.embedded {
		title = paint(cOrange, "▍") + paint(cOrange+bold, "SESSION  ") + paint(cBright+bold, oneLine(a.DisplayName)) + dim("  ·  keys go to Claude Code · ctrl+] or click Agents to come back")
	}
	out := []string{fit(title, w)}
	for _, s := range l.lines() {
		out = append(out, s+"\x1b[0m")
	}
	return out
}
