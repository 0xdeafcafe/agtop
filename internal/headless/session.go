package headless

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/0xdeafcafe/agtop/internal/claude"
)

// Options says which conversation to run and where.
type Options struct {
	Account claude.Account
	Dir     string
	// Resume continues a saved conversation by its session id. SessionID
	// instead names a new one; leave both empty to let Claude Code pick.
	Resume         string
	SessionID      string
	Model          string
	Effort         string // low, medium, high, xhigh, max; fixed for the process's life
	PermissionMode string // default, acceptEdits, plan, auto, ...
	Flags          []string
	// Binary is the claude executable; empty means "claude" on PATH.
	Binary string
	// Tap, when set, sees every output line before it is decoded.
	Tap func(line []byte)
}

func (o Options) args() []string {
	args := []string{"-p",
		"--input-format", "stream-json", "--output-format", "stream-json",
		"--include-partial-messages", "--verbose",
		// stdio routes every permission prompt to us as a can_use_tool request;
		// without it headless sessions deny them silently.
		"--permission-prompt-tool", "stdio",
	}
	if o.Resume != "" {
		args = append(args, "--resume", o.Resume)
	} else if o.SessionID != "" {
		args = append(args, "--session-id", o.SessionID)
	}
	if o.Model != "" {
		args = append(args, "--model", o.Model)
	}
	if o.Effort != "" {
		args = append(args, "--effort", o.Effort)
	}
	if o.PermissionMode != "" {
		args = append(args, "--permission-mode", o.PermissionMode)
	}
	return append(args, o.Flags...)
}

// Session is one running `claude -p`. Read its Events until the channel
// closes; Err then says why it ended.
type Session struct {
	Events <-chan Event

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	wmu    sync.Mutex
	seq    atomic.Int64
	done   chan struct{}
	err    error
	stderr tail
}

// Start launches Claude Code for o.
func Start(o Options) (*Session, error) {
	bin := o.Binary
	if bin == "" {
		bin = "claude"
	}
	cmd := exec.Command(bin, o.args()...)
	cmd.Dir = o.Dir
	cmd.Env = o.Account.Env()
	// Its own process group, so stopping the session takes its shells too.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	events := make(chan Event, 256)
	s := &Session{Events: events, cmd: cmd, stdin: stdin, done: make(chan struct{})}
	cmd.Stderr = &s.stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go s.read(stdout, events, o.Tap)
	return s, nil
}

func (s *Session) read(r io.Reader, events chan<- Event, tap func([]byte)) {
	defer close(s.done)
	defer close(events)
	sc := bufio.NewScanner(r)
	// Tool results and file reads can make single lines very long.
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		if tap != nil {
			tap(sc.Bytes())
		}
		ev, err := Decode(sc.Bytes())
		if err != nil {
			continue
		}
		if o, ok := ev.(Other); ok && o.Type == "control_request" {
			// A request we never registered for; refuse it rather than
			// leave Claude Code waiting.
			var e envelope
			_ = json.Unmarshal(o.Raw, &e)
			_ = s.reply(e.RequestID, nil, "not supported by agtop")
			continue
		}
		events <- ev
	}
	err := s.cmd.Wait()
	if err == nil {
		err = sc.Err()
	}
	if err != nil {
		if t := s.stderr.String(); t != "" {
			err = fmt.Errorf("%w: %s", err, t)
		}
	}
	s.err = err
}

// PID is Claude Code's process id.
func (s *Session) PID() int { return s.cmd.Process.Pid }

// Done closes when the process has exited.
func (s *Session) Done() <-chan struct{} { return s.done }

// Err is why the session ended, once Done has closed.
func (s *Session) Err() error {
	<-s.done
	return s.err
}

func (s *Session) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err = s.stdin.Write(append(b, '\n'))
	return err
}

// Send queues a user message. Sent mid-turn, Claude Code picks it up at its
// next step, as typing into a busy session does.
func (s *Session) Send(text string) error {
	return s.write(map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": text},
	})
}

// Allow lets a requested tool run. input is the tool input to use, usually
// the request's own; always adds the request's suggested rules so Claude
// Code stops asking for the same thing.
func (s *Session) Allow(req PermissionRequest, input json.RawMessage, always bool) error {
	if input == nil {
		input = req.Input
	}
	body := map[string]any{"behavior": "allow", "updatedInput": input, "toolUseID": req.ToolUseID}
	if always && len(req.Suggestions) > 0 {
		body["updatedPermissions"] = req.Suggestions
		body["decisionClassification"] = "user_permanent"
	} else {
		body["decisionClassification"] = "user_temporary"
	}
	return s.reply(req.ID, body, "")
}

// Deny refuses a requested tool. message is what Claude sees; interrupt also
// stops the turn.
func (s *Session) Deny(req PermissionRequest, message string, interrupt bool) error {
	if message == "" {
		message = "The user declined this tool call."
	}
	return s.reply(req.ID, map[string]any{
		"behavior": "deny", "message": message, "interrupt": interrupt,
		"toolUseID": req.ToolUseID, "decisionClassification": "user_reject",
	}, "")
}

func (s *Session) reply(id string, body any, errText string) error {
	resp := map[string]any{"subtype": "success", "request_id": id, "response": body}
	if errText != "" {
		resp = map[string]any{"subtype": "error", "request_id": id, "error": errText}
	}
	return s.write(map[string]any{"type": "control_response", "response": resp})
}

// control sends a request to Claude Code and returns its id; the answer
// arrives later as a ControlReply event.
func (s *Session) control(req map[string]any) (string, error) {
	id := fmt.Sprintf("agtop-%d", s.seq.Add(1))
	return id, s.write(map[string]any{"type": "control_request", "request_id": id, "request": req})
}

// Initialize asks for the session's slash commands, models and account; the
// answer is a ControlReply for the returned id (read it with Commands).
func (s *Session) Initialize() (string, error) {
	return s.control(map[string]any{"subtype": "initialize"})
}

// Interrupt stops the current turn, as esc does.
func (s *Session) Interrupt() error {
	_, err := s.control(map[string]any{"subtype": "interrupt", "reason": "interrupt"})
	return err
}

// SetPermissionMode switches between default, acceptEdits, plan, auto, ...
func (s *Session) SetPermissionMode(mode string) error {
	_, err := s.control(map[string]any{"subtype": "set_permission_mode", "mode": mode})
	return err
}

// SetModel switches model for the next turn; empty resets to the default.
func (s *Session) SetModel(model string) error {
	req := map[string]any{"subtype": "set_model"}
	if model != "" {
		req["model"] = model
	}
	_, err := s.control(req)
	return err
}

// Stop ends the session. Closing stdin lets Claude Code finish writing the
// transcript; it is killed if it has not gone after grace.
func (s *Session) Stop(grace time.Duration) error {
	s.wmu.Lock()
	_ = s.stdin.Close()
	s.wmu.Unlock()
	select {
	case <-s.done:
	case <-time.After(grace):
		_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGTERM)
		select {
		case <-s.done:
		case <-time.After(2 * time.Second):
			_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
			<-s.done
		}
	}
	var exit *exec.ExitError
	if errors.As(s.err, &exit) {
		return nil // stopped on purpose
	}
	return s.err
}

// tail keeps the last few KB of stderr for error messages.
type tail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if n := len(t.buf); n > 4<<10 {
		t.buf = t.buf[n-4<<10:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}
