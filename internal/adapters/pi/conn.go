package pi

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/agent/event"
	"github.com/0xdeafcafe/agtop/internal/agent/usage"
)

// model is Pi's Model, as much as agtop reads of it.
type model struct {
	ID            string `json:"id"`
	Provider      string `json:"provider"`
	ContextWindow int    `json:"contextWindow"`
}

// state is get_state's answer.
type state struct {
	Model       *model `json:"model"`
	SessionFile string `json:"sessionFile"`
	SessionID   string `json:"sessionId"`
	IsStreaming bool   `json:"isStreaming"`
}

// dialog is an extension's select or confirm, waiting on the user.
type dialog struct {
	confirm bool
}

// Conn is a Pi session run through `pi --mode rpc`.
type Conn struct {
	rpc    *client
	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	session  string
	model    model
	running  bool      // between agent_start and agent_end
	began    time.Time // the running run's start
	msg      string    // the assistant message being written
	stop     string    // the run's last stop reason, and its error
	failed   string
	last     string           // the run's last words
	tokens   usage.TokenUsage // the run's
	cost     float64          // the process's, all told
	sent     []string         // messages sent that pi hasn't said back yet
	dialogs  map[string]dialog
	compacts int // compactions asked for and not yet done

	qmu    sync.Mutex
	queue  []event.Event
	wake   chan struct{}
	events chan event.Event
	quit   chan struct{}
	once   sync.Once
}

var (
	_ agent.Answerer = (*Conn)(nil)
	_ agent.PIDer    = (*Conn)(nil)
	_ agent.Ender    = (*Conn)(nil)
)

// validID is a session id pi takes with --session-id.
var validID = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?$`)

// Start starts, resumes or forks a session: PI_CODING_AGENT_DIR is the
// profile's folder, the model is a pattern pi matches ("provider/id" or an
// id), and effort is its thinking level. Pi has no modes; o.Mode is let be.
func Start(ctx context.Context, o *agent.StartOptions) (*Conn, error) {
	args, err := argsFor(o)
	if err != nil {
		return nil, err
	}
	binary := o.Binary
	if binary == "" {
		binary = "pi"
		if _, err := exec.LookPath(binary); err != nil {
			if p := agent.Path(Kind); p != "" {
				binary = p
			}
		}
	}
	cmd := exec.Command(binary, args...)
	cmd.Dir = o.Dir
	cmd.Env = append(os.Environ(), "PI_SKIP_VERSION_CHECK=1")
	if o.Profile.Dir != "" {
		cmd.Env = append(cmd.Env, "PI_CODING_AGENT_DIR="+o.Profile.Dir)
	}
	if o.Lean {
		cmd.Env = append(cmd.Env, "PI_OFFLINE=1")
	}
	if o.TempDir != "" && os.MkdirAll(o.TempDir, 0o700) == nil {
		cmd.Env = append(cmd.Env, "TMPDIR="+o.TempDir)
	}
	cmd.Env = append(cmd.Env, o.Env...)
	c := newConn(ctx)
	rpc, err := spawn(cmd, c.handle)
	if err != nil {
		c.cancel()
		return nil, err
	}
	if err := c.begin(rpc, o); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

// argsFor is pi's command line for o.
func argsFor(o *agent.StartOptions) ([]string, error) {
	args := []string{"--mode", "rpc"}
	switch {
	case o.Fork || o.Resume:
		if o.SessionID == "" {
			return nil, errors.New("pi: no session to resume")
		}
		path := sessionFile(o.Profile, o.SessionID)
		if path == "" {
			return nil, fmt.Errorf("pi: no session %s in %s", o.SessionID, o.Profile.Dir)
		}
		flag := "--session"
		if o.Fork {
			flag = "--fork"
		}
		args = append(args, flag, path)
	case o.SessionID != "" && validID.MatchString(o.SessionID):
		args = append(args, "--session-id", o.SessionID)
	}
	if o.Model != "" {
		args = append(args, "--model", o.Model)
	}
	if o.Effort != "" {
		args = append(args, "--thinking", o.Effort)
	}
	if p := strings.TrimSpace(o.Prompt); p != "" {
		args = append(args, "--append-system-prompt", p)
	}
	return append(args, o.Flags...), nil
}

func newConn(ctx context.Context) *Conn {
	c := &Conn{dialogs: map[string]dialog{}, wake: make(chan struct{}, 1),
		events: make(chan event.Event, 64), quit: make(chan struct{})}
	c.ctx, c.cancel = context.WithCancel(ctx)
	return c
}

// startFor is how long pi has to load its extensions and say it's ready.
const startFor = time.Minute

// begin asks pi where it stands, which is also how agtop knows it's up.
func (c *Conn) begin(rpc *client, o *agent.StartOptions) error {
	c.rpc = rpc
	go c.pump()
	go func() {
		select {
		case <-c.ctx.Done():
			_ = c.Close()
		case <-c.quit:
		}
	}()
	ctx, cancel := context.WithTimeout(c.ctx, startFor)
	defer cancel()
	var st state
	if err := rpc.call(ctx, map[string]any{"type": "get_state"}, &st); err != nil {
		if said := rpc.said(); said != "" && errors.Is(err, errClosed) {
			return fmt.Errorf("pi: %s", lastLine(said))
		}
		return err
	}
	c.mu.Lock()
	c.session = st.SessionID
	if st.Model != nil {
		c.model = *st.Model
	}
	m := c.model.ID
	c.mu.Unlock()
	c.emit(event.Init{SessionID: st.SessionID, Model: m, Cwd: o.Dir})
	if c.model.ContextWindow > 0 {
		c.emit(event.Context{Window: c.model.ContextWindow})
	}
	go c.readCommands()
	return nil
}

// readCommands lists the session's commands once: prompt templates,
// skills and extensions' commands, all sent as a prompt starting with /.
func (c *Conn) readCommands() {
	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer cancel()
	var res struct {
		Commands []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"commands"`
	}
	if c.rpc.call(ctx, map[string]any{"type": "get_commands"}, &res) != nil || len(res.Commands) == 0 {
		return
	}
	list := make([]event.Command, len(res.Commands))
	for i, cmd := range res.Commands {
		list[i] = event.Command{Name: cmd.Name, Description: cmd.Description}
	}
	c.emit(event.Commands{List: list})
}

func (c *Conn) Events() <-chan event.Event { return c.events }

// callFor is how long a command pi answers at once has.
const callFor = 30 * time.Second

// Send sends a message: a prompt when pi is idle, else one it steers the
// running work with, after the tools it's running. /compact compacts.
func (c *Conn) Send(in agent.Input) error {
	if rest, ok := compactCommand(in.Text); ok {
		c.mu.Lock()
		c.compacts++
		c.mu.Unlock()
		go c.compact(rest)
		return nil
	}
	cmd := map[string]any{"type": "prompt", "message": in.Text}
	if len(in.Images) > 0 {
		imgs, err := images(in.Images)
		if err != nil {
			return err
		}
		cmd["images"] = imgs
	}
	c.mu.Lock()
	busy := c.running
	c.sent = append(c.sent, in.Text)
	c.mu.Unlock()
	if busy {
		cmd["streamingBehavior"] = "steer"
	}
	ctx, cancel := context.WithTimeout(c.ctx, callFor)
	defer cancel()
	err := c.rpc.call(ctx, cmd, nil)
	if re, ok := errors.AsType[*rpcError](err); ok && !busy && strings.Contains(re.Message, "already processing") {
		// It started on something of its own meanwhile.
		cmd["streamingBehavior"] = "steer"
		err = c.rpc.call(ctx, cmd, nil)
	}
	if err != nil {
		c.mu.Lock()
		if n := len(c.sent); n > 0 && c.sent[n-1] == in.Text {
			c.sent = c.sent[:n-1]
		}
		c.mu.Unlock()
	}
	return err
}

// compactCommand is whether text is /compact, and what's after it.
func compactCommand(text string) (string, bool) {
	t := strings.TrimSpace(text)
	if t != "/compact" && !strings.HasPrefix(t, "/compact ") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(t, "/compact")), true
}

// compactFor is how long a compaction has: the model summarises the
// whole conversation.
const compactFor = 10 * time.Minute

// compact compacts the conversation, and ends the turn /compact began
// when nothing else is running.
func (c *Conn) compact(instructions string) {
	cmd := map[string]any{"type": "compact"}
	if instructions != "" {
		cmd["customInstructions"] = instructions
	}
	ctx, cancel := context.WithTimeout(c.ctx, compactFor)
	defer cancel()
	err := c.rpc.call(ctx, cmd, nil)
	c.mu.Lock()
	c.compacts--
	running, cost := c.running, c.cost
	c.mu.Unlock()
	if running {
		return
	}
	end := event.TurnEnd{Reason: "done", Cost: cost}
	if err != nil {
		end.Reason, end.Err = "error", err.Error()
		if re, ok := errors.AsType[*rpcError](err); ok {
			end.Err = re.Message
		}
	}
	c.emit(end)
}

// images are image files as pi takes them: base64, with their type.
func images(paths []string) ([]map[string]any, error) {
	var out []map[string]any
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		mt := mimeOf(p, b)
		out = append(out, map[string]any{"type": "image", "data": base64.StdEncoding.EncodeToString(b), "mimeType": mt})
	}
	return out, nil
}

func mimeOf(path string, b []byte) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	}
	return http.DetectContentType(b)
}

// Interrupt aborts what pi is doing. Pi answers once it has stopped, so
// the answer isn't waited for.
func (c *Conn) Interrupt() error {
	return c.rpc.post(map[string]any{"type": "abort"})
}

// SetModel switches model at once: "provider/id", or an id, looked for
// first with the provider in use.
func (c *Conn) SetModel(name string) error {
	ctx, cancel := context.WithTimeout(c.ctx, callFor)
	defer cancel()
	var res struct {
		Models []model `json:"models"`
	}
	if err := c.rpc.call(ctx, map[string]any{"type": "get_available_models"}, &res); err != nil {
		return err
	}
	c.mu.Lock()
	provider := c.model.Provider
	c.mu.Unlock()
	m, ok := findModel(res.Models, name, provider)
	if !ok {
		return fmt.Errorf("pi: no model %q", name)
	}
	var set model
	if err := c.rpc.call(ctx, map[string]any{"type": "set_model", "provider": m.Provider, "modelId": m.ID}, &set); err != nil {
		return err
	}
	if set.ID == "" {
		set = m
	}
	c.mu.Lock()
	c.model = set
	c.mu.Unlock()
	if set.ContextWindow > 0 {
		c.emit(event.Context{Window: set.ContextWindow})
	}
	return nil
}

// findModel is the model name means: "provider/id" exactly, else an id,
// the provider's own first.
func findModel(models []model, name, provider string) (model, bool) {
	for _, m := range models {
		if m.Provider+"/"+m.ID == name {
			return m, true
		}
	}
	var found *model
	for i, m := range models {
		if m.ID != name {
			continue
		}
		if m.Provider == provider {
			return m, true
		}
		if found == nil {
			found = &models[i]
		}
	}
	if found != nil {
		return *found, true
	}
	return model{}, false
}

// SetMode: Pi has no permission modes.
func (c *Conn) SetMode(m string) error {
	return errors.New("pi has no permission modes: it runs every tool without asking")
}

// Answer: Pi asks no approvals; an extension's questions are answered with
// AnswerQuestion.
func (c *Conn) Answer(approvalID, optionID string) error {
	return fmt.Errorf("pi: nothing is waiting on %s: Pi asks no approvals", approvalID)
}

// AnswerQuestion answers an extension's select or confirm with the label
// chosen; none cancels it.
func (c *Conn) AnswerQuestion(id string, answers map[string][]string) error {
	c.mu.Lock()
	d, ok := c.dialogs[id]
	delete(c.dialogs, id)
	c.mu.Unlock()
	if !ok {
		return fmt.Errorf("pi: nothing is waiting on %s", id)
	}
	var chosen string
	for _, v := range answers {
		if len(v) > 0 {
			chosen = v[0]
			break
		}
	}
	reply := map[string]any{"type": "extension_ui_response", "id": id}
	switch {
	case chosen == "":
		reply["cancelled"] = true
	case d.confirm:
		reply["confirmed"] = chosen == confirmYes
	default:
		reply["value"] = chosen
	}
	return c.rpc.post(reply)
}

// PID is pi's process.
func (c *Conn) PID() int {
	if c.rpc == nil || c.rpc.cmd == nil || c.rpc.cmd.Process == nil {
		return 0
	}
	return c.rpc.cmd.Process.Pid
}

// Err is why pi went, as it last said on stderr, when it went by itself.
func (c *Conn) Err() error {
	select {
	case <-c.quit:
		return nil // closed
	default:
	}
	if c.rpc == nil {
		return nil
	}
	if said := c.rpc.said(); said != "" {
		return fmt.Errorf("pi: %s", lastLine(said))
	}
	return nil
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[i+1:])
	}
	return s
}

// Close ends pi and the session with it.
func (c *Conn) Close() error {
	c.once.Do(func() {
		close(c.quit)
		c.cancel()
		if c.rpc != nil {
			_ = c.rpc.close()
		}
	})
	return nil
}

// emit queues events for Events, so the reader never waits on whoever
// draws them.
func (c *Conn) emit(evs ...event.Event) {
	if len(evs) == 0 {
		return
	}
	c.qmu.Lock()
	c.queue = append(c.queue, evs...)
	c.qmu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *Conn) pump() {
	defer close(c.events)
	for {
		c.qmu.Lock()
		q := c.queue
		c.queue = nil
		c.qmu.Unlock()
		for _, e := range q {
			select {
			case c.events <- e:
			case <-c.quit:
				return
			}
		}
		if len(q) > 0 {
			continue
		}
		select {
		case <-c.wake:
		case <-c.quit:
			return
		case <-c.rpc.done:
			c.qmu.Lock()
			done := len(c.queue) == 0
			c.qmu.Unlock()
			if done {
				return
			}
		}
	}
}
