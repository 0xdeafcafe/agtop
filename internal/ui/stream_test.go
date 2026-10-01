package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/rush/internal/adapters/claude/headless"
	"github.com/0xdeafcafe/rush/internal/cellw"
	"github.com/0xdeafcafe/rush/internal/convo"
	"github.com/0xdeafcafe/rush/internal/fleet"
	"github.com/0xdeafcafe/rush/internal/host"
)

// A line after a quiet spell is handed over at once; lines in a burst
// gather for at most a frame; the replay gathers whole.
func TestHostLinesLatency(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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

	})
}

// A followed transcript that grows is noticed within a few polls, not on
// the next second's tick.
func TestTranscriptWatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(path, []byte(`{"type":"user","timestamp":"2026-09-23T20:00:00Z","message":{"role":"user","content":"hi"}}`+"\n"), 0o644)
	tl := convo.NewTail(path)
	tl.Read()
	m := &Model{snap: &fleet.Snapshot{}, host: &hostConn{kind: "claude", key: "k", tail: tl, sess: tl.Sess}}
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
		m.drain(m.onGrow(msg.(growMsg)))
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
				for _, imgs := range []map[int]string{nil, {1: "/tmp/a.png"}} {
					m.input, m.paneFocus, m.imgs = []rune(in+"[Image #1]"), focus, imageRefs{N: len(imgs), Path: imgs}
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
	anchor := c.top
	for i := 0; i < 20; i++ {
		c.sess.Apply(host.Sent{Text: fmt.Sprintf("more %d", i)}, time.Now())
		c.sess.Apply(headless.Message{Role: "assistant", ID: fmt.Sprintf("x%d", i), Blocks: []headless.Block{{Type: "text", Text: "and more"}}}, time.Now())
		c.sess.Apply(headless.Result{Subtype: "success"}, time.Now())
		m.View()
	}
	// When the turn finishes its activity dock disappears, giving the
	// transcript more rows at the bottom. Its top anchor must stay put.
	if !shows() || c.top.ref != anchor.ref || c.top.off != anchor.off {
		t.Fatalf("the window moved: anchor %+v -> %+v\n%v\n%v", anchor, c.top, top, c.rowRefs)
	}
	// Scrolling yourself still moves it.
	c.scroll += 5
	m.View()
	if fmt.Sprint(c.rowRefs) == fmt.Sprint(top) {
		t.Fatal("a scroll should move the window")
	}
}

// Connecting takes the host's whole replay in before the pane first draws,
// so the first frame is the conversation's end, not its start.
func TestReplayBeforeFirstFrame(t *testing.T) {
	lines := make(chan []byte, 512)
	for i := 1; i <= 60; i++ {
		lines <- fmt.Appendf(nil, `{"agtop_sent":true,"message":{"content":"question %d","role":"user"},"type":"user"}`, i)
		lines <- fmt.Appendf(nil, `{"type":"assistant","message":{"id":"m%d","role":"assistant","content":[{"type":"text","text":"answer number %d"}]}}`, i, i)
		lines <- []byte(`{"type":"result","subtype":"success"}`)
	}
	lines <- []byte(`{"info":{},"type":"agtop_info"}`)
	lines <- []byte(`{"type":"result","subtype":"success"}`) // live output after it stays for next
	sess := convo.New()
	if !takeReplay(lines, sess, time.Second, nil) {
		t.Fatal("the replay should have come in whole")
	}
	if len(lines) != 1 {
		t.Fatalf("what follows the replay is left for the pane: %d lines", len(lines))
	}
	m, _ := benchModel(120, 40)
	m.host = &hostConn{kind: "claude", key: m.host.key, client: &host.Client{Lines: lines}, sess: sess, open: map[string]bool{}, ready: true}
	v := ansi.Strip(m.render())
	if !strings.Contains(v, "answer number 60") || strings.Contains(v, "answer number 1\n") {
		t.Fatalf("the first frame should be the conversation's end:\n%s", v)
	}

	// A host that never says it's done: the pane draws what came, in time.
	quiet := make(chan []byte, 1)
	quiet <- []byte(`{"type":"result","subtype":"success"}`)
	if takeReplay(quiet, convo.New(), 20*time.Millisecond, nil) {
		t.Fatal("no info, no whole replay")
	}
}

// Following the end, the last row is always in view, even when the window's
// top falls inside a long message of yours.
func TestFollowingKeepsTheEnd(t *testing.T) {
	for i := 0; i < 120; i++ {
		reps, n := 10+i/20*2, i%20
		m, _ := benchModel(120, 40)
		c := m.host
		c.sess.Apply(headless.Result{Subtype: "success"}, time.Now())
		c.sess.Apply(host.Sent{Text: strings.Repeat("a long ask that wraps over many rows ", reps)}, time.Now())
		c.sess.Apply(headless.Message{Role: "assistant", ID: "end", Blocks: []headless.Block{{Type: "text", Text: strings.Repeat("line\n\n", n)}}}, time.Now())
		c.sess.Apply(headless.Result{Subtype: "success"}, time.Now())
		c.scroll = 0
		m.View()
		if c.scroll != 0 {
			continue
		}
		if last := c.rowBody[len(c.rowBody)-1]; last != len(c.shown)-1 {
			t.Fatalf("%d asks, %d lines: the last row drawn is %d of %d", reps, n, last, len(c.shown))
		}
	}
}

// A row inset under its project has the header's columns, lined up: at a
// width where the header has CPU, so does the row, under it.
func TestInsetRowMatchesHeader(t *testing.T) {
	m, _ := benchModel(200, 50)
	const w, inset = 98, 4
	head := ansi.Strip(m.columnHeader(w))
	var a *fleet.Agent
	for _, x := range m.snap.Agents {
		if x.PID != 0 && x.Live() {
			a = x
			break
		}
	}
	a.CreatedAt = m.snap.At.Add(-time.Minute) // a time that fits its column
	row := ansi.Strip(strings.Repeat(" ", inset) + m.agentLine(a, w-inset, w, false, 28, false, ""))
	col := func(s, sub string) int { return cellw.String(s[:strings.Index(s, sub)+len(sub)]) }
	if !strings.Contains(row, "%") {
		t.Fatalf("the row has no CPU column under the header's:\n%s\n%s", head, row)
	}
	if got, want := col(row, "%"), col(head, "CPU"); got != want {
		t.Errorf("the row's CPU ends at %d, the header's at %d:\n%s\n%s", got, want, head, row)
	}
}

// Opening a row keeps it where it is on screen and grows below it, even
// followed to the end, where the window would otherwise push it up.
func TestOpeningGrowsDown(t *testing.T) {
	m, _ := benchModel(120, 40)
	c := m.host
	m.View()
	firstY := func(ref string) int {
		for y, r := range c.rowRefs {
			if r == ref {
				return y
			}
		}
		return -1
	}
	for y := c.bodyTop; y < len(c.rowRefs); y++ {
		ref := c.rowRefs[y]
		if ref == "" || m.isOpen(c, ref) || firstY(ref) != y {
			continue
		}
		before := len(c.rowRefs)
		c.keepRow(ref)
		c.open[ref] = true
		m.View()
		if got := firstY(ref); got != y {
			t.Fatalf("%s moved from row %d to %d on opening (rows %d -> %d)", ref, y, got, before, len(c.rowRefs))
		}
		return
	}
	t.Skip("nothing closed in view to open")
}

// Enter opens and closes the row picked when nothing's typed, as space
// does; it isn't only for sending.
func TestEnterOpensThePickedRow(t *testing.T) {
	m, _ := benchModel(120, 40)
	c := m.host
	m.paneFocus = true
	m.View()
	for _, ref := range c.rowRefs[c.bodyTop:] {
		if ref == "" || strings.Contains(ref, ":u:") || strings.HasPrefix(ref, "sub:") || m.isOpen(c, ref) {
			continue
		}
		c.sel, c.input = ref, nil
		m.paneKey(tea.KeyPressMsg{Code: tea.KeyEnter}, "enter")
		if !m.isOpen(c, ref) {
			t.Fatalf("enter should open %s", ref)
		}
		return
	}
	t.Skip("nothing closed in view to open")
}
