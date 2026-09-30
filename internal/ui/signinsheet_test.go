package ui

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/agent/usage"
	"github.com/0xdeafcafe/rush/internal/fleet"
	"github.com/0xdeafcafe/rush/internal/state"
)

// fakeLogin prints its link as Claude Code does (an OSC 8 link), asks for
// a code, and takes only good#state.
const fakeLogin = `printf 'Opening browser to sign in…\n'
printf 'If the browser did not open, visit: \033]8;;https://example.test/oauth?code=true&state=s1\007https://example.test/oauth?code=true&state=s1\033]8;;\007\n'
printf 'Paste code here if prompted > '
while read line; do
  [ "$line" = "good#state" ] && { echo 'Login successful.'; exit 0; }
  echo 'Invalid code. Please make sure the full code was copied.' >&2
done
exit 1`

// openSignIn opens the sheet on script and starts it as the UI would.
func openSignIn(t *testing.T, m *Model, script string) *signInSheet {
	t.Helper()
	start := func() (*exec.Cmd, func(error) tea.Msg, error) {
		return exec.Command("sh", "-c", script), func(error) tea.Msg { return doneMsg{text: "signed in"} }, nil
	}
	cmd := m.signIn("Test", start)
	s := m.sheet.(*signInSheet)
	cmd().(sheetMsg).apply(m)
	if s.run == nil {
		t.Fatalf("didn't start: %v", s.err)
	}
	return s
}

// pump feeds the run's events to the sheet until until holds, or it ends:
// then it's what the end did.
func pump(t *testing.T, m *Model, s *signInSheet, until func() bool) tea.Cmd {
	t.Helper()
	for r := s.run; ; {
		select {
		case e := <-r.ev:
			cmd := s.on(m, r, e)
			if _, end := e.(signInEnd); end || until() {
				return cmd
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out; it printed %q", s.out)
		}
	}
}

func TestSignInSheet(t *testing.T) {
	m := &Model{store: &state.Store{}, w: 120, h: 40}
	s := openSignIn(t, m, fakeLogin)
	pump(t, m, s, func() bool { return s.asks && s.url != "" })
	if s.url != "https://example.test/oauth?code=true&state=s1" {
		t.Fatalf("link %q", s.url)
	}

	s.paste("bad#state\n")
	s.key(m, tea.KeyPressMsg{}, "enter")()
	pump(t, m, s, func() bool { return !s.sent })
	if !strings.Contains(s.said(), "Invalid code") || m.sheet != s {
		t.Fatalf("after a bad code: says %q, sheet %T", s.said(), m.sheet)
	}

	s.paste(" good#state ")
	s.key(m, tea.KeyPressMsg{}, "enter")()
	done := pump(t, m, s, func() bool { return false })
	if m.sheet != nil || done == nil {
		t.Fatalf("signed in: sheet %T", m.sheet)
	}
	if msg, ok := done().(doneMsg); !ok || msg.text != "signed in" {
		t.Fatalf("finished with %#v", done())
	}

	// esc cancels, and the program goes with it.
	s = openSignIn(t, m, fakeLogin)
	pump(t, m, s, func() bool { return s.url != "" })
	s.key(m, tea.KeyPressMsg{}, "esc")
	if pump(t, m, s, func() bool { return false }); m.sheet != nil || s.run.cmd.ProcessState == nil {
		t.Fatalf("esc: sheet %T, process %v", m.sheet, s.run.cmd.ProcessState)
	}

	// One that ends without a link couldn't be followed: the terminal has it.
	s = openSignIn(t, m, "echo 'not a terminal' >&2; exit 1")
	if cmd := pump(t, m, s, func() bool { return false }); m.sheet != nil || cmd == nil {
		t.Fatalf("no link: sheet %T, terminal %v", m.sheet, cmd != nil)
	}
}

// An expired login's row says r refreshes it.
func TestExpiredLoginRow(t *testing.T) {
	m := &Model{}
	r := acctRow{login: &fleet.LoginView{Login: state.Login{ID: "id-1"}}, q: usage.Quota{Problem: "couldn't tell whose sign-in it is: " + usage.Expired}}
	if got := m.problem(r); got != "sign-in expired · r refreshes it" {
		t.Fatalf("row says %q", got)
	}
}
