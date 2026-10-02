// Package kanbanvault is the bundled plugin that keeps secrets out of what
// you send: before a message holding one goes, it asks to save it to
// Kanban Code's vault with kv, and the message then goes with
// {{vault:NAME}} in its place. Detection is package secrets, LangWatch's
// redaction rules; the question is rush's ask, the answer a plugin gives
// to ui.intercept.
package kanbanvault

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/0xdeafcafe/rush/internal/bundled/kanbanvault/secrets"
	"github.com/0xdeafcafe/rush/internal/jsonx"
	"github.com/0xdeafcafe/rush/internal/plugin"
)

func init() { plugin.RegisterBundle(plugin.Bundle{Optional: true, Manifest: Manifest, Run: Run}) }

// Name is the plugin's.
const Name = "kanban-vault"

// Manifest is the kanban-vault plugin's.
var Manifest = plugin.Manifest{
	Name: Name,
	Description: "Asks before a message holding a secret (an API key, a token, a password in a URL) goes, and saves it to Kanban Code's vault: " +
		"the message then goes with {{vault:NAME}} in its place, and agents use it through kv run. Off until you turn it on; needs Kanban Code's kv.",
	Command:  []string{"rush"},
	UI:       []string{plugin.UIInput, plugin.UIIntercept, plugin.UINotify},
	Exec:     map[string][]string{"kv": {"~/.local/bin/kv"}},
	MemoryMB: 64,
}

// Rules is what the vault tells an agent about a secret pasted into a
// prompt.
const Rules = "Pasted into a chat prompt by the user; use it only for the task that prompt asks for, never print or copy it."

// namesFor is how long the vault's names, as last listed, are good for
// naming a secret in a question. A save lists them again first.
const namesFor = time.Minute

// Vault is Kanban Code's vault, as the plugin uses it.
type Vault interface {
	Names(ctx context.Context) ([]string, error)
	Add(ctx context.Context, name, value string) error
}

type app struct {
	conn  *plugin.Conn
	ready chan struct{}
	vault Vault

	mu      sync.Mutex
	names   []string
	namesAt time.Time
	listing bool
	asked   map[string]asked           // questions out, by id
	letGo   map[string]map[string]bool // secrets you said to send as they are, by box
}

// asked is a question out about one secret: to save it under name, or,
// failed, whether to send it as it is.
type asked struct {
	box, value, kind, suggested, name string
	failed                            bool
}

// Run is the plugin, on its connection to the broker.
func Run(rw io.ReadWriteCloser) error {
	a := newApp(nil)
	a.conn = plugin.NewConn(rw, a.handle)
	a.vault = kvExec{a.conn}
	close(a.ready)
	<-a.conn.Done()
	return nil
}

// Handler is the plugin as a handler of what the broker asks it, with v
// as its vault: the plugin without a connection, for a window to try it.
func Handler(v Vault) plugin.Handler {
	a := newApp(v)
	close(a.ready)
	return a.handle
}

func newApp(v Vault) *app {
	return &app{ready: make(chan struct{}), vault: v, asked: map[string]asked{}, letGo: map[string]map[string]bool{}}
}

type uiEvent struct {
	Kind    string            `json:"kind"`
	Session *plugin.UISession `json:"session"`
	Text    string            `json:"text"`
}

func (a *app) handle(ctx context.Context, method string, params jsontext.Value) (any, error) {
	<-a.ready
	switch method {
	case "initialize":
		return map[string]any{}, nil
	case "tools.list":
		return map[string]any{"tools": []any{}}, nil
	case "ui.settings":
		return nil, nil
	case "ui.event":
		var ev uiEvent
		if jsonx.Unmarshal(params, &ev) == nil {
			a.event(ev)
		}
		return nil, nil
	case "ui.intercept":
		var in plugin.Intercept
		if err := jsonx.Unmarshal(params, &in); err != nil {
			return nil, &plugin.Error{Code: plugin.CodeInvalidParams, Message: err.Error()}
		}
		return a.intercept(in), nil
	case "ui.intercept.answer":
		var in struct {
			plugin.Intercept
			ID  string `json:"id"`
			Key string `json:"key"`
		}
		if err := jsonx.Unmarshal(params, &in); err != nil {
			return nil, &plugin.Error{Code: plugin.CodeInvalidParams, Message: err.Error()}
		}
		return a.answer(ctx, in.Intercept, in.ID, in.Key), nil
	}
	return nil, &plugin.Error{Code: plugin.CodeNoMethod, Message: "method not found: " + method}
}

// event lists the vault's names as soon as a secret is typed, so the
// question when it's sent can name it at once; a box sent or cleared
// forgets the secrets let go from it.
func (a *app) event(ev uiEvent) {
	box := ""
	if ev.Session != nil {
		box = ev.Session.ID
	}
	switch ev.Kind {
	case plugin.EvInputChanged:
		if len(secrets.Find(ev.Text)) > 0 {
			a.listSoon()
		}
	case plugin.EvInputSent, plugin.EvInputCleared:
		a.mu.Lock()
		delete(a.letGo, box)
		a.mu.Unlock()
	}
}

// listSoon lists the vault's names in the background, unless they're
// fresh or already being listed.
func (a *app) listSoon() {
	a.mu.Lock()
	if a.listing || time.Since(a.namesAt) < namesFor {
		a.mu.Unlock()
		return
	}
	a.listing = true
	a.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		names, err := a.vault.Names(ctx)
		a.mu.Lock()
		defer a.mu.Unlock()
		a.listing = false
		if err == nil {
			a.names, a.namesAt = names, time.Now()
		}
	}()
}

// next is the first secret in text not let go from box.
func (a *app) next(box, text string) *secrets.Detected {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, d := range secrets.Find(text) {
		if !a.letGo[box][d.Value] {
			return &d
		}
	}
	return nil
}

// intercept asks about the first secret in the message, if there's one.
// It must answer at once, so the name it offers is free among the names
// last listed; a save checks again.
func (a *app) intercept(in plugin.Intercept) plugin.InterceptResult {
	d := a.next(in.Box, in.Text)
	if d == nil {
		return plugin.InterceptResult{Action: "allow"}
	}
	a.listSoon()
	a.mu.Lock()
	names := a.names
	a.mu.Unlock()
	return a.offer(in.Box, *d, secrets.UniqueName(d.SuggestedName, names), plugin.InterceptResult{})
}

// offer asks to save d under name, with the changes so far.
func (a *app) offer(box string, d secrets.Detected, name string, changes plugin.InterceptResult) plugin.InterceptResult {
	id := a.remember(asked{box: box, value: d.Value, kind: d.Kind, suggested: d.SuggestedName, name: name})
	changes.Action, changes.ID = "ask", id
	changes.Question = "Save as vault secret " + name + "?"
	changes.Detail = fmt.Sprintf("your message has a secret (%s, %s) · saved, it goes as %s and agents use it through kv run",
		strings.ReplaceAll(d.Kind, "_", " "), Hint(d.Value), secrets.Ref(name))
	changes.Choices = []plugin.AskChoice{{Key: "y", Label: "save it", Enter: true}, {Key: "n", Label: "send as is", Esc: true}}
	return changes
}

// failed says why the secret couldn't be saved, and asks to send the
// message as it is or go back to it.
func (a *app) failed(q asked, err error, changes plugin.InterceptResult) plugin.InterceptResult {
	q.failed = true
	changes.Action, changes.ID = "ask", a.remember(q)
	changes.Question = "Couldn't save the secret to the vault"
	changes.Detail = err.Error()
	changes.Choices = []plugin.AskChoice{{Key: "y", Label: "send as is"}, {Key: "n", Label: "back to the box", Esc: true}}
	return changes
}

func (a *app) remember(q asked) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	a.mu.Lock()
	a.asked[id] = q
	a.mu.Unlock()
	return id
}

// answer acts on the key chosen in a question: y saves the secret and
// puts its reference in its place, n lets it go as it is; then the next
// secret in the message is asked about, if there's one.
func (a *app) answer(ctx context.Context, in plugin.Intercept, id, key string) plugin.InterceptResult {
	a.mu.Lock()
	q, ok := a.asked[id]
	delete(a.asked, id)
	a.mu.Unlock()
	if !ok {
		return plugin.InterceptResult{Action: "block", Reason: "the question was from before a restart: send it again"}
	}
	letGo := func() {
		a.mu.Lock()
		if a.letGo[q.box] == nil {
			a.letGo[q.box] = map[string]bool{}
		}
		a.letGo[q.box][q.value] = true
		a.mu.Unlock()
	}
	switch {
	case q.failed && key == "y", !q.failed && key == "n":
		letGo()
		return a.then(q.box, in.Text, plugin.InterceptResult{})
	case q.failed:
		return plugin.InterceptResult{Action: "block", Reason: "not sent; it's still in the box"}
	case key != "y":
		return plugin.InterceptResult{Action: "block", Reason: "no such answer: " + key}
	}
	names, err := a.vault.Names(ctx)
	if err != nil {
		return a.failed(q, err, plugin.InterceptResult{})
	}
	a.mu.Lock()
	a.names, a.namesAt = names, time.Now()
	a.mu.Unlock()
	// Taken since it was offered: offer the next free one.
	if name := secrets.UniqueName(q.suggested, names); name != q.name {
		return a.offer(q.box, secrets.Detected{Value: q.value, Kind: q.kind, SuggestedName: q.suggested}, name, plugin.InterceptResult{})
	}
	if err := a.vault.Add(ctx, q.name, q.value); err != nil {
		return a.failed(q, err, plugin.InterceptResult{})
	}
	a.mu.Lock()
	a.names = append(slices.Clone(a.names), q.name)
	a.mu.Unlock()
	a.notify("saved " + q.name + " to the vault")
	return a.then(q.box, in.Text, Swap(in.Text, q.value, q.name))
}

// then is changes to text, asking about the next secret left in it if
// there's one.
func (a *app) then(box, text string, changes plugin.InterceptResult) plugin.InterceptResult {
	text = changes.Apply(text)
	if d := a.next(box, text); d != nil {
		a.mu.Lock()
		names := a.names
		a.mu.Unlock()
		return a.offer(box, *d, secrets.UniqueName(d.SuggestedName, names), changes)
	}
	if len(changes.Replace) == 0 && changes.Append == "" {
		return plugin.InterceptResult{Action: "allow"}
	}
	changes.Action = "rewrite"
	return changes
}

// Swap is the change that puts name's reference in place of value in
// text, with the line on using it at the end: secrets.Replace, said as a
// replacement and what it appends, so a window keeps the box's chips.
func Swap(text, value, name string) plugin.InterceptResult {
	whole := secrets.Replace(text, value, name)
	replaced := strings.ReplaceAll(text, value, secrets.Ref(name))
	return plugin.InterceptResult{Replace: []plugin.Replacement{{Old: value, New: secrets.Ref(name)}}, Append: strings.TrimPrefix(whole, replaced)}
}

func (a *app) notify(text string) {
	if a.conn == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.conn.Call(ctx, "ui.notify", map[string]any{"text": text}, nil)
	}()
}

// Hint names a secret without showing it: its first 4 characters and its
// length.
func Hint(v string) string {
	r := []rune(v)
	return fmt.Sprintf("%s… %d chars", string(r[:min(4, len(r))]), len(r))
}

// kvExec is the vault through Kanban Code's kv CLI, which rush runs for
// the plugin as you (exec), with your environment.
type kvExec struct{ conn *plugin.Conn }

type execOut struct {
	Code   int    `json:"code"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

func (k kvExec) run(ctx context.Context, stdin string, args ...string) (string, error) {
	var out execOut
	if err := k.conn.Call(ctx, "exec", map[string]any{"name": "kv", "args": args, "stdin": stdin}, &out); err != nil {
		if msg := err.Error(); strings.Contains(msg, "fork/exec") && strings.Contains(msg, "no such file") {
			return "", errors.New("kv isn't installed: Kanban Code installs it in ~/.local/bin")
		}
		return "", err
	}
	if out.Code != 0 {
		if s := strings.TrimSpace(out.Stderr); s != "" {
			return out.Stdout, errors.New(strings.TrimPrefix(s, "kv: "))
		}
		return out.Stdout, fmt.Errorf("kv %s exited %d", args[0], out.Code)
	}
	return out.Stdout, nil
}

func (k kvExec) Names(ctx context.Context) ([]string, error) {
	out, err := k.run(ctx, "", "ls", "--json")
	if err != nil {
		return nil, err
	}
	var list []struct {
		Name string `json:"name"`
	}
	if err := jsonx.Unmarshal([]byte(out), &list); err != nil {
		return nil, fmt.Errorf("kv ls: %w", err)
	}
	names := make([]string, 0, len(list))
	for _, s := range list {
		names = append(names, s.Name)
	}
	slices.Sort(names)
	return names, nil
}

func (k kvExec) Add(ctx context.Context, name, value string) error {
	_, err := k.run(ctx, value, "add", name, "--tier", "judged", "--rules", Rules)
	return err
}
