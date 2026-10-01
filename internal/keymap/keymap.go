// Package keymap names what rush's keys do, so you can move them.
//
// Every key rush answers to in the Agents list, a Session or anywhere is an
// Action with the keys it has by default. keybindings.json says which keys
// you gave an action instead, and those can be chords: "ctrl+x d" is ctrl+x
// then d. A plugin's commands and rush's own # commands are actions too,
// with no key until you (or, where the key is free, the plugin) give one.
//
// rush's key handling stays written against the default keys. A Map sits in
// front of it and turns each key you press into the default key of the
// action you bound it to: bind session.send to ctrl+x s and ctrl+x s
// arrives as ctrl+enter. A default key you moved away from does nothing, unless
// it types a character. An action with nothing to stand in for, a command,
// is handed back to be run, as is one that Runs.
package keymap

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"
)

// Context is where a key applies.
type Context string

const (
	// Global keys work from anywhere, over sheets and the command bar too.
	Global Context = "global"
	// List is the Agents list and the Prompt under it.
	List Context = "list"
	// Prompt is the Prompt with something typed in it: its keys come
	// before the list's, which have them back once it's empty.
	Prompt Context = "prompt"
	// Session is an agent's Session, its conversation and message box.
	Session Context = "session"
	// Pages is a place's pages where there's no list and Session to go
	// between: Efficiency, Settings, Projects and the Wall.
	Pages Context = "pages"
	// Any is for commands: they run the same from the list or a Session.
	Any Context = "any"
)

// Contexts in the order Settings shows them.
var Contexts = []Context{Global, List, Prompt, Session, Pages, Any}

// Title is how Settings names a context.
func (c Context) Title() string {
	switch c {
	case Global:
		return "Everywhere"
	case List:
		return "Agents and the Prompt"
	case Prompt:
		return "The Prompt, with something typed"
	case Session:
		return "A Session"
	case Pages:
		return "Pages: Efficiency, Settings, Projects, Wall"
	case Any:
		return "Commands"
	}
	return string(c)
}

// Action is something a key can do.
type Action struct {
	ID      string  // list.open, session.send, command:stash, plugin:haven.open
	Context Context //
	Title   string  // what it does, in a few words
	// Keys are its default keys. The first that isn't a chord is the one
	// a key bound to it stands in for; the rest are other keys rush
	// already takes for it.
	Keys []string
	// Source is who added it: "" for rush, else the plugin's name.
	Source string
	// Runs is handed back to be run, as a command is, though it has keys:
	// for one whose key is another's in a context below it, or whose keys
	// are all chords, so no key can stand in for it.
	Runs bool
}

// Command is whether running it means calling back rather than standing in
// for a default key.
func (a Action) Command() bool { return len(a.Keys) == 0 || a.Runs }

// standIn is the default key its other keys arrive as: the first that
// isn't a chord, as rush's handling is written against one key.
func (a Action) standIn() string {
	for _, k := range a.Keys {
		if !strings.Contains(k, " ") {
			return k
		}
	}
	return ""
}

// Seq is a key sequence: one key, or a chord of several.
type Seq []string

func (s Seq) String() string { return strings.Join(s, " ") }

// Parse reads a key sequence as keybindings.json writes it: keys by
// bubbletea's names (ctrl+x, alt+shift+up, enter, space), a space between
// the keys of a chord. cmd+ and meta+ are read as super+.
func Parse(s string) (Seq, error) {
	f := strings.Fields(s)
	if len(f) == 0 {
		return nil, errors.New("an empty key")
	}
	if len(f) > 3 {
		return nil, fmt.Errorf("%q: a chord of at most 3 keys", s)
	}
	out := make(Seq, len(f))
	for i, k := range f {
		n, err := normal(k)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", s, err)
		}
		out[i] = n
	}
	return out, nil
}

var mods = []string{"ctrl", "alt", "shift", "super"}

// normal puts a key's modifiers in bubbletea's order, ctrl+alt+shift+super.
func normal(k string) (string, error) {
	if k == "+" {
		return k, nil
	}
	parts := strings.Split(k, "+")
	base := parts[len(parts)-1]
	if base == "" {
		// "ctrl++" is ctrl and the + key.
		if strings.HasSuffix(k, "++") {
			parts, base = parts[:len(parts)-2], "+"
		} else {
			return "", fmt.Errorf("key %q has no key after its modifiers", k)
		}
	} else {
		parts = parts[:len(parts)-1]
	}
	has := map[string]bool{}
	for _, p := range parts {
		p = strings.ToLower(p)
		switch p {
		case "cmd", "meta", "command":
			p = "super"
		case "option", "opt":
			p = "alt"
		case "control":
			p = "ctrl"
		}
		if !slices.Contains(mods, p) {
			return "", fmt.Errorf("key %q: %q is not a modifier (use ctrl, alt, shift or cmd)", k, p)
		}
		has[p] = true
	}
	if utf8.RuneCountInString(base) > 1 {
		base = strings.ToLower(base)
	}
	var b strings.Builder
	for _, m := range mods {
		if has[m] {
			b.WriteString(m + "+")
		}
	}
	b.WriteString(base)
	return b.String(), nil
}

// Types is whether a key on its own types a character: one you can't take
// for an action as the first key of a sequence, as the box would lose it.
func Types(k string) bool {
	if k == "space" {
		return true
	}
	if strings.Contains(k, "+") && k != "+" && !strings.HasPrefix(k, "shift+") {
		return false
	}
	return utf8.RuneCountInString(strings.TrimPrefix(k, "shift+")) == 1
}

// File is keybindings.json: for each action you changed, its keys. An empty
// list unbinds it.
type File struct {
	Bindings map[string][]string `json:"bindings"`
}

// Problem is a binding in the file that couldn't be used, and why.
type Problem struct {
	Action, Key, Why string
}

func (p Problem) String() string {
	if p.Key == "" {
		return p.Action + ": " + p.Why
	}
	return p.Action + " on " + p.Key + ": " + p.Why
}

// Map is the keys in force.
type Map struct {
	actions map[string]Action
	order   []string
	// bound is, per context, each sequence's action.
	bound map[Context]map[string]string
	// keys is each action's sequences in force.
	keys map[string][]Seq
	// freed is, per context, default keys no longer bound to anything.
	freed    map[Context]map[string]bool
	prefixes map[Context]map[string]bool
	// yours are the actions keybindings.json sets.
	yours    map[string]bool
	problems []Problem
}

// Build makes the Map from the actions, what you set, and what plugins
// suggest. Your bindings win over rush's defaults; a plugin's suggestion
// is taken only where the key is free.
func Build(actions []Action, file File, suggested map[string][]string) *Map {
	m := &Map{
		actions:  map[string]Action{},
		bound:    map[Context]map[string]string{},
		keys:     map[string][]Seq{},
		freed:    map[Context]map[string]bool{},
		prefixes: map[Context]map[string]bool{},
		yours:    map[string]bool{},
	}
	for _, a := range actions {
		if _, dup := m.actions[a.ID]; dup {
			continue
		}
		m.actions[a.ID] = a
		m.order = append(m.order, a.ID)
	}
	for _, c := range Contexts {
		m.bound[c] = map[string]string{}
		m.freed[c] = map[string]bool{}
		m.prefixes[c] = map[string]bool{}
	}
	m.bindDefaults(file)
	m.bindYours(file)
	m.bindSuggested(file, suggested)
	m.markFreed()
	return m
}

// bindDefaults binds rush's keys, except for actions you set.
func (m *Map) bindDefaults(file File) {
	for _, id := range m.order {
		if _, yours := file.Bindings[id]; yours {
			continue
		}
		a := m.actions[id]
		for _, k := range a.Keys {
			s, err := Parse(k)
			if err != nil {
				m.problems = append(m.problems, Problem{id, k, err.Error()})
				continue
			}
			m.bind(a, s)
		}
	}
}

// bindYours binds keybindings.json's keys, in a steady order, each taking
// its key from a default that had it.
func (m *Map) bindYours(file File) {
	for _, id := range slices.Sorted(maps.Keys(file.Bindings)) {
		a, ok := m.actions[id]
		if !ok {
			// A plugin that's gone, say. Kept in the file, not used.
			m.problems = append(m.problems, Problem{id, "", "no such action"})
			continue
		}
		m.yours[id] = true
		for _, k := range file.Bindings[id] {
			m.bindYour(a, k)
		}
	}
}

func (m *Map) bindYour(a Action, k string) {
	s, err := Parse(k)
	if err != nil {
		m.problems = append(m.problems, Problem{a.ID, k, err.Error()})
		return
	}
	why, take := m.cannot(a, s, true)
	if why != "" {
		m.problems = append(m.problems, Problem{a.ID, s.String(), why})
		return
	}
	for _, t := range take {
		m.unbind(t[0], t[1])
	}
	m.bind(a, s)
}

// bindSuggested binds what plugins suggest for their own commands, only
// where nothing else is.
func (m *Map) bindSuggested(file File, suggested map[string][]string) {
	for _, id := range slices.Sorted(maps.Keys(suggested)) {
		a, ok := m.actions[id]
		if _, yours := file.Bindings[id]; !ok || a.Source == "" || yours {
			continue
		}
		for _, k := range suggested[id] {
			if s, err := Parse(k); err == nil {
				if why, _ := m.cannot(a, s, false); why == "" {
					m.bind(a, s)
				}
			}
		}
	}
}

// markFreed notes the default keys that no longer do anything.
func (m *Map) markFreed() {
	for _, id := range m.order {
		a := m.actions[id]
		for _, k := range a.Keys {
			s, err := Parse(k)
			if err != nil || len(s) != 1 {
				continue
			}
			for _, c := range scope(a.Context) {
				if _, ok := m.bound[c][s[0]]; !ok && !m.prefixes[c][s[0]] {
					m.freed[c][s[0]] = true
				}
			}
		}
	}
}

// scope is the contexts an action's keys are looked up in.
func scope(c Context) []Context {
	if c == Any {
		return []Context{List, Session}
	}
	return []Context{c}
}

// clash is the contexts whose keys an action's keys would shadow or be
// shadowed by.
func clash(c Context) []Context {
	switch c {
	case Global:
		return []Context{Global, List, Prompt, Session}
	case Any, List, Prompt, Session:
		return append(scope(c), Global)
	}
	return nil
}

// cannot says why a can't have s, or "". A binding of yours takes the key
// from a default that had it, so what it would take is returned to unbind.
func (m *Map) cannot(a Action, s Seq, yours bool) (string, [][2]string) {
	if Types(s[0]) && a.Context != Global && !slices.Contains(a.Keys, s[0]) {
		return "it types a character; start a chord with a key that doesn't, ctrl+x say", nil
	}
	var take [][2]string
	for _, c := range clash(a.Context) {
		key := s.String()
		if other, ok := m.bound[c][key]; ok && other != a.ID {
			if !yours || m.yours[other] {
				return "already " + other, nil
			}
			take = append(take, [2]string{other, key})
		}
		// A chord and a key that starts it can't both be bound.
		for i := 1; i < len(s); i++ {
			if other, ok := m.bound[c][s[:i].String()]; ok && other != a.ID {
				if !yours || m.yours[other] {
					return s[:i].String() + " is already " + other + ", so no chord can start with it", nil
				}
				take = append(take, [2]string{other, s[:i].String()})
			}
		}
		if m.prefixes[c][key] {
			return "a chord already starts with " + key, nil
		}
	}
	return "", take
}

func (m *Map) bind(a Action, s Seq) {
	for _, c := range scope(a.Context) {
		m.bound[c][s.String()] = a.ID
		for i := 1; i < len(s); i++ {
			m.prefixes[c][s[:i].String()] = true
		}
	}
	m.keys[a.ID] = append(m.keys[a.ID], s)
}

func (m *Map) unbind(id, key string) {
	a := m.actions[id]
	for _, c := range scope(a.Context) {
		if m.bound[c][key] == id {
			delete(m.bound[c], key)
		}
	}
	m.keys[id] = slices.DeleteFunc(m.keys[id], func(s Seq) bool { return s.String() == key })
}

// Problems are the bindings in the file that aren't in force.
func (m *Map) Problems() []Problem { return m.problems }

// Actions are every action, in the order given.
func (m *Map) Actions() []Action {
	out := make([]Action, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, m.actions[id])
	}
	return out
}

// Action is the action with id.
func (m *Map) Action(id string) (Action, bool) {
	a, ok := m.actions[id]
	return a, ok
}

// Keys are the key sequences an action has now.
func (m *Map) Keys(id string) []Seq { return m.keys[id] }

// KeyText is an action's keys for a hint: "ctrl+s" or "ctrl+s · ctrl+x s",
// or "" for none.
func (m *Map) KeyText(id string) string {
	out := make([]string, 0, len(m.keys[id]))
	for _, s := range m.keys[id] {
		out = append(out, s.String())
	}
	return strings.Join(out, " · ")
}

// Changed is whether keybindings.json sets the action's keys.
func (m *Map) Changed(id string) bool { return m.yours[id] }

// Result is what a key does.
type Result struct {
	// Key is the key to hand on: the one pressed, or the default key of the
	// action it's bound to. "" means nothing: it's dropped.
	Key string
	// Run is a command action to run instead, when set.
	Run string
	// Pending is a chord begun: the keys so far, waiting for the next.
	Pending Seq
	// Missed is a chord that was begun but that no action finishes.
	Missed Seq
}

// Resolve is what key does in ctxs (the most specific first; Global is
// always looked at last), after the keys of a chord pending.
func (m *Map) Resolve(ctxs []Context, pending Seq, key string) Result {
	seq := append(slices.Clone(pending), key)
	s := seq.String()
	all := append(slices.Clone(ctxs), Global)
	for _, c := range all {
		if id, ok := m.bound[c][s]; ok {
			a := m.actions[id]
			if a.Command() {
				return Result{Run: id}
			}
			// One of its own keys arrives as it is: rush's handling
			// may tell them apart (ctrl+\ moves even from a box).
			if len(seq) == 1 && slices.Contains(a.Keys, s) {
				return Result{Key: s}
			}
			return Result{Key: a.standIn()}
		}
	}
	for _, c := range all {
		if m.prefixes[c][s] {
			return Result{Pending: seq}
		}
	}
	if len(pending) > 0 {
		return Result{Missed: seq}
	}
	for _, c := range all {
		if m.freed[c][key] && !Types(key) {
			return Result{}
		}
	}
	return Result{Key: key}
}

// Starts is whether any chord in ctxs starts with key, so a caller can tell
// a key that begins one from any other.
func (m *Map) Starts(ctxs []Context, key string) bool {
	for _, c := range append(slices.Clone(ctxs), Global) {
		if m.prefixes[c][key] {
			return true
		}
	}
	return false
}

// Conflicts lists what a new binding of s to id would clash with, for
// Settings to show before it's saved.
func (m *Map) Conflicts(id string, s Seq) []string {
	a, ok := m.actions[id]
	if !ok {
		return nil
	}
	var out []string
	if Types(s[0]) && a.Context != Global && !slices.Contains(a.Keys, s[0]) {
		out = append(out, "types a character")
	}
	for _, c := range clash(a.Context) {
		if other, ok := m.bound[c][s.String()]; ok && other != id {
			out = append(out, other)
		}
		for i := 1; i < len(s); i++ {
			if other, ok := m.bound[c][s[:i].String()]; ok && other != id {
				out = append(out, other)
			}
		}
		for k, other := range m.bound[c] {
			if other != id && strings.HasPrefix(k, s.String()+" ") {
				out = append(out, other)
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}
