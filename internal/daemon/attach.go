package daemon

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/charmbracelet/x/term"
	"golang.org/x/sys/unix"
)

var (
	detachMarker = []byte("\x1b_cc-daemon-detach\x1b\\")
	hintMarker   = []byte("\x1b_cc-daemon-hint\x1b\\")
)

const escapeKey = 0x1d // ctrl+]

// Session is a tea.ExecCommand: Bubble Tea releases the terminal, Run relays
// the live session, and the list comes back when the session asks to detach.
type Session struct {
	Client Client
	Short  string
	stdin  io.Reader
	stdout io.Writer
}

func (s *Session) SetStdin(r io.Reader)  { s.stdin = r }
func (s *Session) SetStdout(w io.Writer) { s.stdout = w }
func (s *Session) SetStderr(io.Writer)   {}

func (s *Session) Run() error {
	in, out := os.Stdin, os.Stdout
	cols, rows, err := term.GetSize(out.Fd())
	if err != nil || cols <= 0 {
		cols, rows = 120, 40
	}
	attachID := fmt.Sprintf("agtop-%d", os.Getpid())
	conn, r, info, err := s.Client.Attach(s.Short, attachID, cols, rows)
	if err != nil {
		return err
	}
	defer conn.Close()

	state, err := term.MakeRaw(in.Fd())
	if err != nil {
		return err
	}
	defer term.Restore(in.Fd(), state)

	// The session repaints relative to a blank screen, so give it one: the
	// alternate screen, cleared, as Claude Code's own attach does. Without it
	// the repaint lands on top of whatever the shell left behind.
	fmt.Fprint(out, "\x1b[?1049h\x1b[H\x1b[2J")
	for _, m := range info.DecModes {
		fmt.Fprintf(out, "\x1b[?%dh", m)
	}
	defer func() {
		// Modes the session turned on while attached are reset too.
		for _, m := range append(info.DecModes, 1000, 1002, 1003, 1004, 1006, 2004) {
			fmt.Fprintf(out, "\x1b[?%dl", m)
		}
		fmt.Fprint(out, "\x1b[<u\x1b[0m\x1b[?25h\x1b[?1049l")
	}()

	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)

	done := make(chan struct{})
	inDone := make(chan struct{})
	outErr := make(chan error, 1)
	go func() { outErr <- pumpOut(r, out) }()
	go func() { pumpIn(in, conn, done); close(inDone) }()
	// The list reads stdin again only once the relay has stopped reading it.
	defer func() { <-inDone }()

	for {
		select {
		case <-winch:
			if c, r, err := term.GetSize(out.Fd()); err == nil {
				_ = s.Client.Resize(s.Short, attachID, c, r)
			}
		case err := <-outErr:
			close(done)
			if err == io.EOF || err == errDetach || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
	}
}

var errDetach = fmt.Errorf("detach")

// Relay copies a session's terminal stream to w with the daemon's control
// markers taken out. It returns nil when the session detaches or ends.
func Relay(r io.Reader, w io.Writer) error {
	err := pumpOut(r, w)
	if err == io.EOF || err == errDetach || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

var kicked = []byte("EKICKED: Session opened in another window")

func pumpOut(r io.Reader, w io.Writer) error {
	buf := make([]byte, 32<<10)
	var carry []byte
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := append(carry, buf[:n]...)
			carry = nil
			if i := bytes.Index(chunk, detachMarker); i >= 0 {
				_, _ = w.Write(stripHints(chunk[:i]))
				return errDetach
			}
			if bytes.Contains(chunk, kicked) {
				return io.EOF
			}
			// Hold back only a tail that could still grow into a marker.
			if k := partialMarker(chunk); k > 0 {
				carry = append([]byte(nil), chunk[len(chunk)-k:]...)
				chunk = chunk[:len(chunk)-k]
			}
			if _, werr := w.Write(stripHints(chunk)); werr != nil {
				return werr
			}
		}
		if err != nil {
			return err
		}
	}
}

// partialMarker is the length of the longest suffix of b that is a proper
// prefix of a marker the daemon sends in-band.
func partialMarker(b []byte) int {
	best := 0
	for _, m := range [][]byte{detachMarker, hintMarker, kicked} {
		for k := min(len(m)-1, len(b)); k > best; k-- {
			if bytes.HasSuffix(b, m[:k]) {
				best = k
				break
			}
		}
	}
	return best
}

func stripHints(b []byte) []byte {
	if !bytes.Contains(b, hintMarker) {
		return b
	}
	return []byte(strings.ReplaceAll(string(b), string(hintMarker), ""))
}

// pumpIn polls stdin so it can stop without swallowing the next keystroke
// that belongs to the list view.
func pumpIn(in *os.File, conn net.Conn, done <-chan struct{}) {
	fd := int(in.Fd())
	buf := make([]byte, 4096)
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	for {
		select {
		case <-done:
			return
		default:
		}
		n, err := unix.Poll(fds, 50)
		if err != nil && err != unix.EINTR {
			return
		}
		if n <= 0 || fds[0].Revents&unix.POLLIN == 0 {
			continue
		}
		k, err := unix.Read(fd, buf)
		if err != nil || k <= 0 {
			if err == unix.EAGAIN || err == unix.EINTR {
				continue
			}
			return
		}
		if i := bytes.IndexByte(buf[:k], escapeKey); i >= 0 {
			_, _ = conn.Write(buf[:i])
			_ = conn.Close()
			return
		}
		if _, err := conn.Write(buf[:k]); err != nil {
			return
		}
	}
}
