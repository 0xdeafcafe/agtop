package ui

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// An agent's own sign-in (claude auth login, codex login) runs in the
// background, with its output on a pipe and a pipe for its stdin: both
// open the browser themselves and print the link, and Claude Code reads a
// pasted code from stdin. One whose output can't be followed goes to the
// terminal, as every sign-in did before.

// signInWait is how long a sign-in has to print its link.
const signInWait = 15 * time.Second

// signInStart makes a sign-in's command, and what's done with how it
// ended; it runs off the UI goroutine.
type signInStart func() (cmd *exec.Cmd, finish func(error) tea.Msg, err error)

type signInSheet struct {
	title string
	start signInStart
	run   *signInRun
	out   string // all it printed
	url   string
	asks  bool // it asked for a code
	sent  bool // a code went, not yet answered
	code  []rune
	err   error // why it failed, once it has
}

// signInRun is the sign-in program running, and what it says as it does.
type signInRun struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	ev  chan any // signInOut, then one signInEnd
}

type signInOut struct{ text string }

// signInEnd is the program done: then is finish's message, when it
// succeeded.
type signInEnd struct {
	err  error
	then tea.Msg
}

// signIn opens the sheet and starts the sign-in.
func (m *Model) signIn(title string, start signInStart) tea.Cmd {
	s := &signInSheet{title: title, start: start}
	m.sheet = s
	return func() tea.Msg {
		run, err := launch(start)
		return sheetMsg{apply: func(m *Model) tea.Cmd {
			switch {
			case err != nil:
				s.err = err
				return nil
			case m.sheet != s:
				run.kill()
				return run.drain(s)
			}
			s.run = run
			return tea.Batch(run.drain(s), tea.Tick(signInWait, func(time.Time) tea.Msg {
				return sheetMsg{apply: func(m *Model) tea.Cmd {
					if m.sheet == s && s.url == "" && s.err == nil {
						return s.toTerminal(m)
					}
					return nil
				}}
			}))
		}}
	}
}

// launch starts a sign-in with its output on a pipe; finish runs, off the
// UI goroutine, once it succeeds.
func launch(start signInStart) (*signInRun, error) {
	cmd, finish, err := start()
	if err != nil {
		return nil, err
	}
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = w, w
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // killed with what it starts
	in, err := cmd.StdinPipe()
	if err == nil {
		err = cmd.Start()
	}
	w.Close()
	if err != nil {
		r.Close()
		return nil, err
	}
	run := &signInRun{cmd: cmd, in: in, ev: make(chan any, 64)}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				run.ev <- signInOut{string(buf[:n])}
			}
			if err != nil {
				break
			}
		}
		r.Close()
		end := signInEnd{err: cmd.Wait()}
		if end.err == nil {
			end.then = finish(nil)
		}
		run.ev <- end
	}()
	return run, nil
}

// drain takes in the run's next event, and the ones after it.
func (r *signInRun) drain(s *signInSheet) tea.Cmd {
	return func() tea.Msg {
		e := <-r.ev
		return sheetMsg{apply: func(m *Model) tea.Cmd { return s.on(m, r, e) }}
	}
}

func (r *signInRun) kill() {
	if r != nil && r.cmd.Process != nil {
		_ = syscall.Kill(-r.cmd.Process.Pid, syscall.SIGTERM)
	}
}

// on takes in what run said. A sheet closed since only drains it.
func (s *signInSheet) on(m *Model, r *signInRun, e any) tea.Cmd {
	live := m.sheet == s && s.run == r
	switch e := e.(type) {
	case signInOut:
		if live {
			s.took(e.text)
		}
		return r.drain(s)
	case signInEnd:
		switch {
		case !live:
		case e.err == nil:
			m.sheet = nil
			return func() tea.Msg { return e.then }
		case s.url == "":
			return s.toTerminal(m)
		default:
			s.err, s.sent = e.err, false
		}
	}
	return nil
}

// took reads what the program printed: its link, and whether it asks for
// a code.
func (s *signInSheet) took(text string) {
	s.out += text
	if s.url == "" {
		s.url = firstURL(s.out)
	}
	if strings.Contains(strings.ToLower(ansi.Strip(s.out)), "paste code") {
		s.asks = true
	}
	if s.sent && strings.TrimSpace(ansi.Strip(text)) != "" {
		s.sent = false // it answered the code
	}
}

// firstURL is the first https link in out: bare, or an OSC 8 link's
// target, which ends at the sequence's terminator.
func firstURL(out string) string {
	i := strings.Index(out, "https://")
	if i < 0 {
		return ""
	}
	u := out[i:]
	if j := strings.IndexAny(u, " \t\r\n\a\x1b\"'<>"); j >= 0 {
		u = u[:j]
	}
	return u
}

// said is the last thing the program printed that isn't its link.
func (s *signInSheet) said() string {
	lines := strings.Split(ansi.Strip(s.out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" && !strings.Contains(l, "http") {
			return l
		}
	}
	return ""
}

// toTerminal stops the background run and hands the terminal to a fresh
// one, as sign-ins went before.
func (s *signInSheet) toTerminal(m *Model) tea.Cmd {
	s.run.kill()
	m.sheet = nil
	return inTerminal(s.start)
}

// inTerminal runs a sign-in with the terminal handed to it.
func inTerminal(start signInStart) tea.Cmd {
	return func() tea.Msg {
		cmd, finish, err := start()
		if err != nil {
			return doneMsg{err: err}
		}
		return tea.ExecProcess(cmd, finish)()
	}
}

func (*signInSheet) width(*Model) int { return 96 }

func (s *signInSheet) body(m *Model, w, h int) []string {
	out := []string{sheetTitle("Sign in", s.title, w), ""}
	status := paint(cYellow, "● ") + dim("starting…")
	switch {
	case s.err != nil:
		status = paint(cRed, "✗ ") + dim("didn't sign in: "+s.err.Error())
	case s.sent:
		status = paint(cYellow, "● ") + dim("checking the code…")
	case s.url == "" && s.run != nil:
		status = paint(cYellow, "● ") + dim("waiting for its sign-in link…")
	case s.asks:
		status = paint(cGreen, "● ") + dim("finish in the browser it opened, or paste the code the page shows")
	case s.url != "":
		status = paint(cGreen, "● ") + dim("finish in the browser it opened")
	}
	out = append(out, "  "+status)
	if s.url != "" {
		out = append(out, "")
		for u := s.url; u != ""; {
			n := min(len(u), max(10, w-4))
			out = append(out, "  "+paint(cBlue, u[:n]))
			u = u[n:]
		}
	}
	if s.asks && s.err == nil {
		out = append(out, "", "  "+paint(cSub, "code  ")+textField([]rune(strings.Repeat("•", len(s.code))), len(s.code), true, "paste the code here", w-10))
	}
	if said := s.said(); said != "" {
		out = append(out, "", "  "+faint(ansi.Truncate("it says: "+said, w-2, "…")))
	}
	if s.err != nil {
		return append(out, "", keysFit(w, "ctrl+t", "try in the terminal", "esc", "close"))
	}
	return append(out, "", keysFit(w, "ctrl+o", "open the link", "ctrl+y", "copy it", "enter", "send the code", "ctrl+t", "use the terminal", "esc", "cancel"))
}

func (s *signInSheet) key(m *Model, k tea.KeyPressMsg, key string) tea.Cmd {
	switch key {
	case "esc", "ctrl+c":
		s.run.kill()
		m.sheet = nil
		if s.err == nil {
			m.flash("sign-in cancelled", false)
		}
	case "ctrl+o":
		if s.url != "" {
			return browse(s.url)
		}
	case "ctrl+y":
		if s.url != "" {
			m.copyText(s.url)
		}
	case "ctrl+t":
		return s.toTerminal(m)
	case "backspace":
		if len(s.code) > 0 {
			s.code = s.code[:len(s.code)-1]
		}
	case "enter":
		if len(s.code) == 0 || s.run == nil || s.err != nil {
			return nil
		}
		line, in := string(s.code)+"\n", s.run.in
		s.code, s.sent = nil, true
		return func() tea.Msg { _, _ = io.WriteString(in, line); return nil }
	default:
		if k.Text != "" && !strings.ContainsAny(k.Text, "\r\n") {
			s.code = append(s.code, []rune(k.Text)...)
		}
	}
	return nil
}

// paste takes a pasted code.
func (s *signInSheet) paste(text string) {
	s.code = append(s.code, []rune(strings.Join(strings.Fields(text), ""))...)
}
