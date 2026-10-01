package antigravity

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/event"
)

type conn struct {
	cmd     *exec.Cmd
	in      io.WriteCloser
	events  chan event.Event
	cancel  context.CancelFunc
	done    chan struct{}
	mu      sync.Mutex
	busy    bool
	prefix  string
	once    sync.Once
	session string
	journal *os.File
}

func launchArgs(o agent.StartOptions) ([]string, error) {
	if o.Fork || len(o.Carry) > 0 {
		return nil, errors.New("Antigravity cannot import or fork a conversation through its streaming interface")
	}
	if o.APIKey != "" {
		return nil, errors.New("configure modelProvider=gemini in Antigravity's settings and GEMINI_API_KEY before using API billing")
	}
	args := []string{"--input-format", "stream-json", "--output-format", "stream-json"}
	if o.Resume {
		if o.SessionID == "" {
			return nil, errors.New("Antigravity resume requires a conversation ID")
		}
		args = append(args, "--conversation", o.SessionID)
	}
	if o.Model != "" {
		args = append(args, "--model", o.Model)
	}
	if o.Effort != "" {
		args = append(args, "--effort", o.Effort)
	}
	switch o.Mode {
	case "":
	case "plan", "accept-edits":
		args = append(args, "--mode", o.Mode)
	case "always-proceed":
		args = append(args, "--dangerously-skip-permissions")
	default:
		return nil, fmt.Errorf("unsupported Antigravity permission mode %q", o.Mode)
	}
	return append(args, o.Flags...), nil
}
func (Adapter) Start(ctx context.Context, o agent.StartOptions) (agent.Conn, error) {
	args, err := launchArgs(o)
	if err != nil {
		return nil, err
	}
	bin := o.Binary
	if bin == "" {
		bin = agent.Path(Kind)
	}
	if bin == "" {
		return nil, errors.New("Antigravity CLI is not installed")
	}
	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = o.Dir
	cmd.Env = append(os.Environ(), o.Env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	in, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		cancel()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		in.Close()
		cancel()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		in.Close()
		cancel()
		return nil, err
	}
	sid := o.SessionID
	if sid == "" {
		sid = rand.Text()
	}
	c := &conn{session: sid, cmd: cmd, in: in, cancel: cancel, events: make(chan event.Event, 64), done: make(chan struct{})}
	go c.read(ctx, out, stderr)
	return c, nil
}
func (c *conn) Events() <-chan event.Event { return c.events }
func (c *conn) PID() int                   { return c.cmd.Process.Pid }
func (c *conn) Send(in agent.Input) error {
	if len(in.Images) > 0 {
		return errors.New("Antigravity streaming input supports text only")
	}
	if strings.HasPrefix(strings.TrimSpace(in.Text), "/") {
		return errors.New("native Antigravity slash commands are unavailable in Rush streaming mode")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.done:
		return errors.New("Antigravity session has ended")
	default:
	}
	if c.busy {
		return errors.New("Antigravity is working; queue this message until the turn ends")
	}
	text := in.Text
	b, err := json.Marshal(map[string]any{"event": "user", "message": map[string]string{"content": text}})
	if err != nil {
		return err
	}
	if _, err = c.in.Write(append(b, '\n')); err != nil {
		return err
	}
	if err = c.saveEvent(event.Message{Role: "user", Parts: []event.Part{{Kind: event.Text, Text: in.Text}}}); err != nil {
		c.cancel()
		return err
	}
	c.prefix = ""
	c.busy = true
	return nil
}
func (c *conn) Answer(string, string) error {
	return errors.New("Antigravity headless permissions are controlled by its native policy")
}
func (c *conn) SetModel(string) error {
	return errors.New("Antigravity model changes require a new runtime")
}
func (c *conn) SetMode(string) error {
	return errors.New("Antigravity permission changes require a new runtime")
}
func (c *conn) Interrupt() error { return c.cmd.Process.Signal(os.Interrupt) }
func (c *conn) Close() error {
	c.once.Do(func() { c.cancel(); _ = syscall.Kill(-c.PID(), syscall.SIGKILL); _ = c.in.Close() })
	<-c.done
	return nil
}
func (c *conn) read(ctx context.Context, out, stderr io.Reader) {
	defer close(c.done)
	defer close(c.events)
	defer c.cancel()
	defer func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.journal != nil {
			_ = c.journal.Close()
		}
	}()
	// Keep diagnostics bounded while continuing to drain the pipe.
	diagnostics := make(chan string, 1)
	go func() {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := stderr.Read(buf)
			if n > 0 && b.Len() < 16384 {
				b.Write(buf[:min(n, 16384-b.Len())])
			}
			if err != nil {
				break
			}
		}
		diagnostics <- b.String()
	}()
	emit := func(e event.Event) bool {
		select {
		case c.events <- e:
			return true
		case <-ctx.Done():
			return false
		}
	}
	p := parser{emit: func(ev event.Event) bool {
		if err := c.remember(ev); err != nil {
			emit(event.TurnEnd{Reason: "error", Err: "saving Antigravity history: " + err.Error()})
			c.cancel()
			return false
		}
		return emit(ev)
	}}
	sawResult := false
	scan := bufio.NewScanner(out)
	scan.Buffer(make([]byte, 64<<10), 8<<20)
	for scan.Scan() {
		ended, err := p.line(scan.Bytes())
		if err != nil {
			emit(event.TurnEnd{Reason: "error", Err: err.Error()})
			c.cancel()
			break
		}
		if ended {
			sawResult = true
			c.mu.Lock()
			c.busy = false
			c.mu.Unlock()
			if err := c.remember(p.end); err != nil {
				emit(event.TurnEnd{Reason: "error", Err: err.Error()})
				c.cancel()
				break
			}
			if !emit(p.end) {
				break
			}
		}
	}
	scanErr := scan.Err()
	err := c.cmd.Wait()
	detail := <-diagnostics
	c.mu.Lock()
	busy := c.busy
	c.busy = false
	c.mu.Unlock()
	if ctx.Err() == nil && (busy || (!sawResult && (err != nil || scanErr != nil))) {
		if strings.TrimSpace(detail) == "" {
			detail = "Antigravity exited before completing its turn"
			if err != nil {
				detail += ": " + err.Error()
			}
			if scanErr != nil {
				detail += ": " + scanErr.Error()
			}
		}
		emit(event.TurnEnd{Reason: "error", Err: strings.TrimSpace(detail)})
	}
}

var _ agent.PIDer = (*conn)(nil)
