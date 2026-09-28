// Command reconnect rebuilds agtop's own retry as a plugin, to show the UI
// hooks are enough for it: when a session stops because the network went
// away, or on an error worth another go, it sends "continue" once the
// network is back, backing off between tries, and says so when it gives
// up. It never continues a session stopped by a usage limit, a login
// problem, or anything else.
//
// It hears what agtop's agent list shows of sessions, never what was said,
// and may send only to sessions in its workspaces that ask before acting.
// agtop's built-in retry stays as it is; this exists to show the API.
//
//	cd plugins/examples/reconnect
//	mkdir -p ~/.config/agtop/plugins/reconnect
//	go build -o ~/.config/agtop/plugins/reconnect/reconnect .
//	cp plugin.json ~/.config/agtop/plugins/reconnect/
//	agtop plugin check reconnect && agtop plugin approve reconnect
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

type session struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type event struct {
	Kind    string   `json:"kind"`
	Session *session `json:"session"`
	Error   *struct {
		Kind     string `json:"kind"`
		Message  string `json:"message"`
		Retrying bool   `json:"retrying"` // agtop continues it itself
	} `json:"error"`
}

type app struct {
	c *conn

	mu sync.Mutex
	r  *Retrier

	kick chan struct{}
}

func main() {
	f, err := ipc()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	a := &app{c: newConn(f), r: NewRetrier(ParseConfig(nil)), kick: make(chan struct{}, 1)}
	go a.loop()
	_ = a.c.serve(f, a.handle)
}

func (a *app) handle(method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case "initialize", "ui.settings":
		var in struct {
			Settings map[string]string `json:"settings"` // initialize
			Values   map[string]string `json:"values"`   // ui.settings
		}
		_ = json.Unmarshal(params, &in)
		v := in.Settings
		if method == "ui.settings" {
			v = in.Values
		}
		a.mu.Lock()
		a.r.Config = ParseConfig(v)
		a.mu.Unlock()
		if method == "initialize" {
			return map[string]any{}, nil
		}
		return nil, nil
	case "tools.list":
		return map[string]any{"tools": []any{}}, nil
	case "ui.event":
		var ev event
		if json.Unmarshal(params, &ev) == nil {
			a.event(ev)
		}
		return nil, nil
	case "ui.command":
		return nil, &rpcError{Code: -32601, Message: "reconnect has no commands"}
	}
	return nil, &rpcError{Code: -32601, Message: "method not found: " + method}
}

// event takes what happened. It runs on the reading goroutine, so it only
// changes the state; loop does the sending.
func (a *app) event(ev event) {
	now := time.Now()
	a.mu.Lock()
	switch ev.Kind {
	case "session.stopped":
		if ev.Session != nil {
			kind := ""
			if ev.Error != nil && (!ev.Error.Retrying || a.r.Alongside) {
				kind = ev.Error.Kind
			}
			a.r.Stopped(ev.Session.ID, ev.Session.Name, kind, now)
		}
	case "turn.started":
		if ev.Session != nil {
			a.r.Started(ev.Session.ID)
		}
	case "network.down":
		a.r.Down()
	case "network.up":
		a.r.Up(now)
	}
	a.mu.Unlock()
	select {
	case a.kick <- struct{}{}:
	default:
	}
}

// loop does what's due, then sleeps until something next is.
func (a *app) loop() {
	for {
		a.mu.Lock()
		due := a.r.Due(time.Now())
		next := a.r.Next()
		a.mu.Unlock()
		for _, d := range due {
			a.do(d)
		}
		wait := time.Hour
		if !next.IsZero() {
			wait = max(time.Until(next), 50*time.Millisecond)
		}
		select {
		case <-a.kick:
		case <-time.After(wait):
		}
	}
}

func (a *app) do(d Action) {
	if d.GiveUp {
		a.notify(d.Session, fmt.Sprintf("gave up after %d tries", d.Try))
		return
	}
	err := a.c.call("ui.send", map[string]any{"session": d.Session, "text": "continue"}, nil)
	if err != nil {
		// Refused (outside the workspaces, a mode that doesn't ask, or
		// over ten a minute) or gone. The rate case (-32002) could go
		// later, but it's rare enough to give up with the rest.
		a.mu.Lock()
		a.r.Forget(d.Session)
		a.mu.Unlock()
		a.notify(d.Session, fmt.Sprintf("can't continue it: %v", err))
		return
	}
	fmt.Fprintf(os.Stderr, "continued %s (try %d)\n", d.Session, d.Try)
}

// notify says something about a session; agtop names the plugin and it.
func (a *app) notify(session, text string) {
	_ = a.c.call("ui.notify", map[string]any{"session": session, "text": text, "tone": "warn"}, nil)
}
