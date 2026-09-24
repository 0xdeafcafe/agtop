package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/cellw"
	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/headless"
	"github.com/0xdeafcafe/agtop/internal/host"
)

// A line after a quiet spell is handed over at once; lines in a burst
// gather for at most a frame; the replay gathers whole.
func TestHostLinesLatency(t *testing.T) {
	lines := make(chan []byte, 64)
	c := &hostConn{key: "k", client: &host.Client{Lines: lines}}

	lines <- []byte("a")
	go func() { time.Sleep(5 * time.Millisecond); lines <- []byte("b") }()
	if msg := c.next()().(hostLinesMsg); len(msg.lines) != 2 {
		t.Fatalf("the replay should gather: %d lines", len(msg.lines))
	}

	c.flushed = time.Now().Add(-time.Second)
	cmd := c.next()
	lines <- []byte("c")
	go func() { time.Sleep(12 * time.Millisecond); lines <- []byte("x") }()
	if msg := cmd().(hostLinesMsg); len(msg.lines) != 1 {
		t.Fatalf("after a quiet spell a line shouldn't wait for more: %d lines", len(msg.lines))
	}
	<-time.After(15 * time.Millisecond)
	<-lines

	c.flushed = time.Now()
	cmd = c.next()
	lines <- []byte("d")
	go func() { time.Sleep(3 * time.Millisecond); lines <- []byte("e") }()
	start := time.Now()
	msg := cmd().(hostLinesMsg)
	if took := time.Since(start); len(msg.lines) != 2 || took > frame+100*time.Millisecond {
		t.Fatalf("in a burst: %d lines in %v", len(msg.lines), took)
	}
}

// A followed transcript that grows is noticed within a few polls, not on
// the next second's tick.
func TestTranscriptWatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(path, []byte(`{"type":"user","timestamp":"2026-09-23T20:00:00Z","message":{"role":"user","content":"hi"}}`+"\n"), 0o644)
	tl := convo.NewTail(path)
	tl.Read()
	m := &Model{snap: &fleet.Snapshot{}, host: &hostConn{key: "k", tail: tl, sess: tl.Sess}}
	cmd := m.syncWatch()
	if cmd == nil || m.syncWatch() != nil {
		t.Fatal("one watch at a time")
	}
	got := make(chan tea.Msg, 1)
	go func() { got <- cmd() }()
	start := time.Now()
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"type":"assistant","timestamp":"2026-09-23T20:00:01Z","message":{"id":"m","role":"assistant","content":[{"type":"text","text":"hello"}]}}` + "\n")
	f.Close()
	select {
	case msg := <-got:
		if _, ok := msg.(growMsg); !ok || time.Since(start) > 200*time.Millisecond {
			t.Fatalf("got %T after %v", msg, time.Since(start))
		}
		m.onGrow(msg.(growMsg))
	case <-time.After(time.Second):
		t.Fatal("the watch never fired")
	}
	if len(tl.Sess.Turns) != 1 || len(tl.Sess.Turns[0].Items) != 1 {
		t.Fatalf("the new line wasn't taken in: %+v", tl.Sess.Turns)
	}
	next := m.syncWatch()
	if next == nil {
		t.Fatal("the watch should start again")
	}
	m.dropHost()
	if next() != nil {
		t.Fatal("a dropped session's watch should end quietly")
	}
}

// Layout counts the header and the Prompt without drawing them; the counts
// must match what's drawn.
func TestLayoutHeights(t *testing.T) {
	m, _ := benchModel(200, 50)
	for _, w := range []int{200, 46} {
		m.w = w
		if len(m.header()) != m.headH() {
			t.Fatalf("at %d columns the header is %d lines, headH says %d", w, len(m.header()), m.headH())
		}
		for i, l := range m.header() {
			if w < narrowHead && cellw.String(l) > w {
				t.Errorf("at %d columns header line %d is %d wide: %q", w, i, cellw.String(l), ansi.Strip(l))
			}
		}
	}
	m.w = 200
	for _, w := range []int{0, 10, 40, 120} {
		for _, in := range []string{"", "short", "/s", "/sort ", strings.Repeat("a long message that wraps ", 40)} {
			for _, focus := range []bool{false, true} {
				for _, imgs := range [][]string{nil, {"/tmp/a.png"}} {
					m.input, m.paneFocus, m.images = []rune(in), focus, imgs
					if got, want := m.promptH(w), len(m.promptLines(w)); got != want {
						t.Fatalf("w=%d input=%d focus=%v images=%d: promptH %d, drawn %d", w, len(in), focus, len(imgs), got, want)
					}
				}
			}
		}
	}
	m.zen = true
	if m.promptH(100) != 0 || len(m.promptLines(100)) != 0 {
		t.Fatal("zen has no Prompt")
	}
}

func TestOneLineShortcut(t *testing.T) {
	for _, s := range []string{"", " ", "a", "a b", "a  b", " a", "a ", "a\tb", "a\nb", "é b", "a b", "x\rz", "a\vb"} {
		want := strings.Join(strings.Fields(strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(s)), " ")
		if got := oneLine(s); got != want {
			t.Errorf("oneLine(%q) = %q, want %q", s, got, want)
		}
	}
}

// Scrolled up to something (a search's match, say), output arriving
// below keeps it where it is: a replay still coming in or an agent at work
// doesn't carry the window down to the end.
func TestScrolledUpStays(t *testing.T) {
	m, _ := benchModel(120, 40)
	c := m.host
	m.jumpInPane("t5")
	m.View()
	shows := func() bool {
		for _, r := range c.rowRefs {
			if r == "t5" {
				return true
			}
		}
		return false
	}
	if !shows() {
		t.Fatal("the jump should show turn 5")
	}
	top := append([]string{}, c.rowRefs...)
	for i := 0; i < 20; i++ {
		c.sess.Apply(host.Sent{Text: fmt.Sprintf("more %d", i)}, time.Now())
		c.sess.Apply(headless.Message{Role: "assistant", ID: fmt.Sprintf("x%d", i), Blocks: []headless.Block{{Type: "text", Text: "and more"}}}, time.Now())
		c.sess.Apply(headless.Result{Subtype: "success"}, time.Now())
		m.View()
	}
	if !shows() || fmt.Sprint(c.rowRefs) != fmt.Sprint(top) {
		t.Fatalf("the window moved:\n%v\n%v", top, c.rowRefs)
	}
	// Scrolling yourself still moves it.
	c.scroll += 5
	m.View()
	if fmt.Sprint(c.rowRefs) == fmt.Sprint(top) {
		t.Fatal("a scroll should move the window")
	}
}
