package main

import (
	"reflect"
	"time"

	tea "charm.land/bubbletea/v2"
)

// driver runs a tea.Model the way a Program would, without a terminal:
// commands run in goroutines, their messages come back to Update on this
// one, and View is read whenever a frame is wanted.
type driver struct {
	m    tea.Model
	msgs chan tea.Msg
}

func drive(m tea.Model, w, h int) *driver {
	d := &driver{m: m, msgs: make(chan tea.Msg, 4096)}
	d.update(tea.WindowSizeMsg{Width: w, Height: h})
	d.run(m.Init())
	return d
}

// run carries out a command, off this goroutine.
func (d *driver) run(c tea.Cmd) {
	if c == nil {
		return
	}
	go func() { d.post(c()) }()
}

// post hands a command's message back, taking batches and sequences apart.
func (d *driver) post(msg tea.Msg) {
	switch msg := msg.(type) {
	case nil:
		return
	case tea.BatchMsg:
		for _, c := range msg {
			d.run(c)
		}
		return
	case tea.QuitMsg:
		return
	}
	// tea.Sequence's message is unexported: a list of commands, in order.
	if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeFor[tea.Cmd]() {
		for i := range v.Len() {
			if c := v.Index(i).Interface().(tea.Cmd); c != nil {
				d.post(c())
			}
		}
		return
	}
	d.msgs <- msg
}

func (d *driver) update(msg tea.Msg) {
	m, c := d.m.Update(msg)
	d.m = m
	d.run(c)
}

// settle takes in messages for a while, drawing frames as a Program would:
// some of what the view does (a modal taking the keys) happens as it draws.
func (d *driver) settle(dur time.Duration) {
	end := time.After(dur)
	fps := time.NewTicker(50 * time.Millisecond)
	defer fps.Stop()
	for {
		select {
		case msg := <-d.msgs:
			d.update(msg)
		case <-fps.C:
			d.m.View()
		case <-end:
			return
		}
	}
}

// press sends keys, letting each land.
func (d *driver) press(keys ...tea.KeyPressMsg) {
	for _, k := range keys {
		d.update(k)
		d.settle(250 * time.Millisecond)
	}
}

func (d *driver) frame() string { return d.m.View().Content }

// key is a key as a terminal sends it: "enter", "ctrl+k", "]", or text.
func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	}
	if len(s) > 5 && s[:5] == "ctrl+" {
		return tea.KeyPressMsg{Code: rune(s[5]), Mod: tea.ModCtrl}
	}
	if len(s) > 4 && s[:4] == "alt+" {
		return tea.KeyPressMsg{Code: rune(s[4]), Mod: tea.ModAlt}
	}
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}

// typed is text as keys.
func typed(s string) []tea.KeyPressMsg {
	var out []tea.KeyPressMsg
	for _, r := range s {
		out = append(out, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return out
}
