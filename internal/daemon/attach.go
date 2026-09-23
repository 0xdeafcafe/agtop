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
	attachID := fmt.Sprintf("agents-%d", os.Getpid())
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

	for _, m := range info.DecModes {
		fmt.Fprintf(out, "\x1b[?%dh", m)
	}
	defer func() {
		for _, m := range info.DecModes {
			fmt.Fprintf(out, "\x1b[?%dl", m)
		}
		fmt.Fprint(out, "\x1b[0m\x1b[?25h")
	}()

	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)

	done := make(chan struct{})
	outErr := make(chan error, 1)
	go func() { outErr <- pumpOut(r, out) }()
	go pumpIn(in, conn, done)

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
			// Keep a possible partial marker for the next read.
			if j := bytes.LastIndexByte(chunk, 0x1b); j >= 0 && len(chunk)-j < len(detachMarker) {
				carry = append([]byte(nil), chunk[j:]...)
				chunk = chunk[:j]
			}
			if _, werr := w.Write(stripHints(chunk)); werr != nil {
				return werr
			}
			if bytes.Contains(chunk, []byte("EKICKED")) {
				return io.EOF
			}
		}
		if err != nil {
			return err
		}
	}
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
