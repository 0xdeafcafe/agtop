// Command autodrafts rebuilds rush's own drafts as a plugin, to show the
// UI hooks are enough for it: it hears what you type in a message box,
// keeps what you left unsent (you left the Session, or cleared the box),
// and puts the newest back when you ask.
//
// It sees everything you type in rush, and keeps it only in its data
// folder, in drafts.json. It has no network and can't start programs.
// rush's own stash is its bundled drafts plugin; this exists to show the API.
//
//	cd plugins/examples/autodrafts
//	mkdir -p ~/.config/rush/plugins/autodrafts
//	go build -o ~/.config/rush/plugins/autodrafts/autodrafts .
//	cp plugin.json ~/.config/rush/plugins/autodrafts/
//	rush plugin check autodrafts && rush plugin approve autodrafts
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// session is what a UI event says of a session; only its id matters here.
type session struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type event struct {
	Kind    string   `json:"kind"`
	UI      string   `json:"ui"`
	Session *session `json:"session"`
	Text    string   `json:"text"`
}

// box is the key of the box an event is about: a Session's by its id, the
// Prompt's as "".
func box(s *session) string {
	if s == nil {
		return ""
	}
	return s.ID
}

type app struct {
	c *conn

	mu    sync.Mutex
	book  *Book
	path  string
	dirty bool // the store changed since it was last written
	kick  chan struct{}

	smu sync.Mutex // one save at a time
}

func main() {
	f, err := ipc()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	a := &app{c: newConn(f), kick: make(chan struct{}, 1)}
	go a.loop()
	_ = a.c.serve(f, a.handle)
}

func (a *app) handle(method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case "initialize":
		var in struct {
			DataDir  string            `json:"dataDir"`
			Settings map[string]string `json:"settings"`
		}
		_ = json.Unmarshal(params, &in)
		dir := in.DataDir
		if dir == "" {
			dir = os.Getenv("RUSH_PLUGIN_DATA")
		}
		a.mu.Lock()
		a.path = filepath.Join(dir, "drafts.json")
		a.book = NewBook(load(a.path), ParseKeep(in.Settings["keep"]))
		a.mu.Unlock()
		return map[string]any{}, nil
	case "tools.list":
		return map[string]any{"tools": []any{}}, nil
	case "ui.settings":
		var in struct {
			Values map[string]string `json:"values"`
		}
		_ = json.Unmarshal(params, &in)
		a.mu.Lock()
		dropped := a.book != nil && a.book.SetKeep(ParseKeep(in.Values["keep"]))
		a.mu.Unlock()
		if dropped {
			a.save()
		}
		return nil, nil
	case "ui.event":
		var ev event
		if json.Unmarshal(params, &ev) == nil {
			a.event(ev)
		}
		return nil, nil
	case "ui.command":
		var in struct {
			Command string   `json:"command"`
			UI      string   `json:"ui"`
			Session *session `json:"session"`
		}
		_ = json.Unmarshal(params, &in)
		return a.command(in.Command, in.UI, in.Session)
	}
	return nil, &rpcError{Code: -32601, Message: "method not found: " + method}
}

// event takes one thing that happened to a box. Events arrive in order,
// on the reading goroutine, so it only notes them: loop writes the file.
func (a *app) event(ev event) {
	now := time.Now()
	key := box(ev.Session)
	a.mu.Lock()
	if a.book == nil {
		a.mu.Unlock()
		return
	}
	dirty := false
	switch ev.Kind {
	case "input.changed":
		a.book.Changed(key, ev.Text, now)
	case "input.cleared":
		dirty = ev.Text != "" && a.book.Keep(key, ev.Text, now)
	case "session.left":
		dirty = a.book.Keep(key, "", now)
	case "input.sent":
		dirty = a.book.Sent(key)
	}
	a.dirty = a.dirty || dirty
	a.mu.Unlock()
	select {
	case a.kick <- struct{}{}:
	default:
	}
}

func (a *app) command(name, ui string, s *session) (any, *rpcError) {
	key := box(s)
	a.mu.Lock()
	if a.book == nil {
		a.mu.Unlock()
		return nil, &rpcError{Code: -32000, Message: "not started yet"}
	}
	switch name {
	case "restore":
		text, ok := a.book.Restore(key)
		a.mu.Unlock()
		if !ok {
			a.notify(ui, "no drafts kept for this box", "dim")
			return map[string]any{}, nil
		}
		a.save()
		// The session id is the box's: "" is the Prompt.
		if err := a.c.call("ui.input.set", map[string]any{"ui": ui, "session": key, "text": text}, nil); err != nil {
			return nil, &rpcError{Code: -32000, Message: "ui.input.set: " + err.Error()}
		}
		return map[string]any{}, nil
	case "list":
		n := a.book.Count(key)
		a.mu.Unlock()
		where := "the Prompt"
		if s != nil {
			where = "this Session"
			if s.Name != "" {
				where = s.Name
			}
		}
		msg := fmt.Sprintf("%d drafts kept for %s", n, where)
		if n == 1 {
			msg = "1 draft kept for " + where
		}
		a.notify(ui, msg, "")
		return map[string]any{}, nil
	}
	a.mu.Unlock()
	return nil, &rpcError{Code: -32601, Message: "no command " + name}
}

func (a *app) notify(ui, text, tone string) {
	_ = a.c.call("ui.notify", map[string]any{"ui": ui, "text": text, "tone": tone}, nil)
}

// loop settles boxes once they've stopped changing, writing autosaves.
func (a *app) loop() {
	for {
		a.mu.Lock()
		var next time.Time
		dirty := false
		if a.book != nil {
			dirty = a.book.Settle(time.Now()) || a.dirty
			a.dirty = false
			next = a.book.NextSettle()
		}
		a.mu.Unlock()
		if dirty {
			a.save()
		}
		wait := time.Hour
		if !next.IsZero() {
			wait = max(time.Until(next), 10*time.Millisecond)
		}
		select {
		case <-a.kick:
		case <-time.After(wait):
		}
	}
}

// save writes what's kept, as it is now.
func (a *app) save() {
	a.smu.Lock()
	defer a.smu.Unlock()
	a.mu.Lock()
	path := a.path
	data, err := json.Marshal(a.book.Store)
	a.mu.Unlock()
	if err != nil || path == "" {
		return
	}
	if err := writeAtomic(path, data); err != nil {
		fmt.Fprintln(os.Stderr, "saving drafts:", err)
	}
}
