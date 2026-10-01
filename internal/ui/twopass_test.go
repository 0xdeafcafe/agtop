package ui

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/convo"
	"github.com/0xdeafcafe/rush/internal/host"
)

// twoPass is a model showing a long transcript opened on its last few
// turns, and the command that reads the whole of it.
func twoPass(t *testing.T) (*Model, *hostConn) {
	t.Helper()
	var b strings.Builder
	for n := 1; n <= 60; n++ {
		at := func(s int) string { return fmt.Sprintf("2026-09-23T%02d:%02d:%02dZ", 10+n/60, n%60, s) }
		fmt.Fprintf(&b, `{"type":"user","timestamp":%q,"message":{"role":"user","content":"question %d"}}`+"\n", at(0), n)
		fmt.Fprintf(&b, `{"type":"assistant","timestamp":%q,"message":{"id":"u%d","role":"assistant","content":[{"type":"tool_use","id":"b%d","name":"Bash","input":{"command":"go test ./pkg%d"}}]}}`+"\n", at(1), n, n, n)
		fmt.Fprintf(&b, `{"type":"user","timestamp":%q,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"b%d","content":"ok"}]},"toolUseResult":{"stdout":"ok","stderr":""}}`+"\n", at(5), n)
		fmt.Fprintf(&b, `{"type":"assistant","timestamp":%q,"message":{"id":"a%d","role":"assistant","content":[{"type":"text","text":"answer %d"}]}}`+"\n", at(6), n, n)
		fmt.Fprintf(&b, `{"type":"system","subtype":"turn_duration","timestamp":%q}`+"\n", at(7))
	}
	path := t.TempDir() + "/s.jsonl"
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ := benchModel(160, 40)
	tl := convo.NewTailFrom(path, 16<<10)
	if _, err := tl.Read(); err != nil || !tl.Sess.Partial {
		t.Fatalf("tail: %v", err)
	}
	c := &hostConn{key: m.sel, kind: "claude", tail: tl, sess: tl.Sess, open: map[string]bool{}, ready: true, path: path}
	m.host, m.paneFocus = c, false
	return m, c
}

// whole is what readWhole brings the pane.
func whole(t *testing.T, c *hostConn) wholeMsg {
	t.Helper()
	msg, ok := c.readWhole(convo.Options{Width: 150, Open: map[string]bool{}})().(wholeMsg)
	if !ok || len(msg.tail.Sess.Turns) != 60 {
		t.Fatal("the whole transcript wasn't read")
	}
	return msg
}

var turnNum, gaps = regexp.MustCompile(`#\d+`), regexp.MustCompile(` +`)

// onScreen is the conversation's rows in view; turn numbers show only once
// the whole is read, so they're left out.
func onScreen(m *Model, c *hostConn) []string {
	m.View()
	end := len(c.shown) - c.scroll
	var out []string
	for _, l := range c.shown[end-c.bodyRows : end] {
		out = append(out, gaps.ReplaceAllString(turnNum.ReplaceAllString(strings.TrimRight(ansi.Strip(l.Text), " "), ""), " "))
	}
	return out
}

// No turn is numbered from the end read so far: #1 there is #50 once the
// whole is, and numbers that jumped would say so.
func TestNumbersOnlyOnceWhole(t *testing.T) {
	m, c := twoPass(t)
	m.View()
	if slices.ContainsFunc(c.shown, func(l convo.Line) bool { return turnNum.MatchString(ansi.Strip(l.Text)) }) {
		t.Error("a turn numbered before the whole was read")
	}
	m.onWhole(whole(t, c))
	m.View()
	// Turn numbers aren't drawn, but each turn's rows carry its number.
	if !slices.ContainsFunc(c.shown, func(l convo.Line) bool { return l.Ref == "t60" }) {
		t.Error("the last turn isn't #60 once the whole is read")
	}
}

func TestWholeKeepsTheBottom(t *testing.T) {
	m, c := twoPass(t)
	before := onScreen(m, c)
	m.onWhole(whole(t, c))
	if c.sess.Partial {
		t.Fatal("the whole didn't swap in")
	}
	if after := onScreen(m, c); !slices.Equal(before, after) {
		t.Errorf("the bottom moved:\n%s\n---\n%s", strings.Join(before, "\n"), strings.Join(after, "\n"))
	}
}

func TestWholeKeepsWhereYouScrolled(t *testing.T) {
	m, c := twoPass(t)
	onScreen(m, c)
	c.scroll = 6
	before := onScreen(m, c)
	m.onWhole(whole(t, c))
	if after := onScreen(m, c); !slices.Equal(before, after) {
		t.Errorf("the view moved:\n%s\n---\n%s", strings.Join(before, "\n"), strings.Join(after, "\n"))
	}
}

func TestCountingUntilWhole(t *testing.T) {
	m, c := twoPass(t)
	c.view = slices.Index(m.views(c), "overview")
	if got := strings.Join(onScreen(m, c), "\n"); !strings.Contains(got, "counting…") {
		t.Errorf("the overview of a part:\n%s", got)
	}
	m.onWhole(whole(t, c))
	if got := strings.Join(onScreen(m, c), "\n"); strings.Contains(got, "counting…") || !strings.Contains(got, "60") {
		t.Errorf("the overview of the whole:\n%s", got)
	}
}

// A hosted session opens in stages, none waiting on the next: the header
// from its host's info, its transcript's end before the replay, that with
// the replay, then the whole, with what the host sent meanwhile on top.
func TestHostedOpensAtLatestWithoutShowingPreReplayPrefix(t *testing.T) {
	at := func(n int) time.Time {
		return time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC).Add(time.Duration(n) * time.Minute)
	}
	out := strings.Repeat("x", 40<<10) // enough turns to run past tailBytes
	var b strings.Builder
	for n := 1; n <= 60; n++ {
		s := at(n).Format(time.RFC3339)
		fmt.Fprintf(&b, `{"type":"user","timestamp":%q,"message":{"role":"user","content":"question %d"}}`+"\n", s, n)
		fmt.Fprintf(&b, `{"type":"assistant","timestamp":%q,"message":{"id":"a%d","role":"assistant","content":[{"type":"text","text":"answer %d %s"}]}}`+"\n", s, n, n, out)
	}
	path := t.TempDir() + "/s.jsonl"
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	// The host replays from turn 51 on.
	pre, _ := preFor(host.Info{Kind: "claude", State: "idle", Model: "opus", StartedAt: at(0), ReplayFrom: at(51)}, nil, host.Config{Resume: true}, nil, path, agent.Profile{})
	lines := make(chan []byte, 64)
	for n := 51; n <= 60; n++ {
		lines <- fmt.Appendf(nil, `{"agtop_sent":true,"message":{"content":"question %d","role":"user"},"type":"user"}`, n)
		lines <- []byte(`{"type":"result","subtype":"success"}`)
	}
	lines <- []byte(`{"info":{"state":"idle","model":"opus"},"type":"agtop_info"}`)
	m, _ := benchModel(160, 40)
	c := &hostConn{key: m.sel, kind: "claude", client: &host.Client{Lines: lines}, sess: pre.base(), open: map[string]bool{}, pre: pre}
	m.host = c
	if c.sess.Info.Model != "opus" {
		t.Fatal("no header from the host's info")
	}
	o := convo.Options{Width: 150, Open: map[string]bool{}}
	prepared := c.readPre(o)().(preMsg)
	if last := prepared.sess.Turns[len(prepared.sess.Turns)-1]; !prepared.sess.Partial || last.Prompt != "question 50" {
		t.Fatal("bounded history before replay was not prepared")
	}
	replay := m.onPre(prepared)
	if len(c.sess.Turns) != 0 || c.ready {
		t.Fatal("an older history prefix was shown before the latest replay arrived")
	}
	cmds := m.onReplay(replay().(replayMsg))().(tea.BatchMsg)
	if last := c.sess.Turns[len(c.sess.Turns)-1]; last.Prompt != "question 60" || !c.ready {
		t.Fatalf("with the replay: %q", last.Prompt)
	}
	lines <- fmt.Appendf(nil, `{"agtop_sent":true,"message":{"content":"question 61","role":"user"},"type":"user"}`)
	m.onHostLines(cmds[0]().(hostLinesMsg)) // live, while the whole is read
	m.onWhole(cmds[1]().(wholeMsg))
	if s := c.sess; s.Partial || len(s.Turns) != 61 || s.Turns[60].Prompt != "question 61" || s.Turns[49].Prompt != "question 50" {
		t.Fatalf("the whole: partial %v, %d turns", s.Partial, len(s.Turns))
	}
}
