package ui

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/rush/internal/convo"
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

var turnNum = regexp.MustCompile(` *#\d+ *`)

// onScreen is the conversation's rows in view; turn numbers settle once
// the whole is read, so they're left out.
func onScreen(m *Model, c *hostConn) []string {
	m.View()
	end := len(c.shown) - c.scroll
	var out []string
	for _, l := range c.shown[end-c.bodyRows : end] {
		out = append(out, turnNum.ReplaceAllString(strings.TrimRight(ansi.Strip(l.Text), " "), " # "))
	}
	return out
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
