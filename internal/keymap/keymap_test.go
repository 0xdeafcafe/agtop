package keymap

import (
	"slices"
	"testing"
)

var acts = []Action{
	{ID: "quit", Context: Global, Keys: []string{"ctrl+q"}},
	{ID: "list.stop", Context: List, Keys: []string{"ctrl+x"}},
	{ID: "list.up", Context: List, Keys: []string{"up"}},
	{ID: "session.send", Context: Session, Keys: []string{"ctrl+s"}},
	{ID: "session.stop", Context: Session, Keys: []string{"ctrl+x"}},
	{ID: "session.enter", Context: Session, Keys: []string{"enter"}},
	{ID: "command:drafts", Context: Any},
	{ID: "plugin:haven.open", Context: Any, Source: "haven"},
}

func TestParse(t *testing.T) {
	for in, want := range map[string]string{
		"ctrl+x d":       "ctrl+x d",
		"Cmd+K":          "super+K",
		"shift+ctrl+up":  "ctrl+shift+up",
		"meta+alt+Enter": "alt+super+enter",
		"ctrl++":         "ctrl++",
		"option+s":       "alt+s",
	} {
		s, err := Parse(in)
		if err != nil || s.String() != want {
			t.Errorf("Parse(%q) = %q, %v; want %q", in, s, err, want)
		}
	}
	for _, bad := range []string{"", "hyper+x", "ctrl+", "a b c d"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) should fail", bad)
		}
	}
}

func TestDefaultsPassThrough(t *testing.T) {
	m := Build(acts, File{}, nil)
	if p := m.Problems(); len(p) != 0 {
		t.Fatal(p)
	}
	if r := m.Resolve([]Context{Session}, nil, "ctrl+s"); r.Key != "ctrl+s" {
		t.Errorf("default = %+v", r)
	}
	if r := m.Resolve([]Context{List}, nil, "x"); r.Key != "x" {
		t.Errorf("typing = %+v", r)
	}
}

func TestRebindStandsInForDefault(t *testing.T) {
	m := Build(acts, File{Bindings: map[string][]string{"session.send": {"ctrl+enter"}}}, nil)
	if r := m.Resolve([]Context{Session}, nil, "ctrl+enter"); r.Key != "ctrl+s" {
		t.Errorf("rebound = %+v", r)
	}
	// ctrl+s no longer sends, and does nothing else.
	if r := m.Resolve([]Context{Session}, nil, "ctrl+s"); r.Key != "" || r.Run != "" {
		t.Errorf("freed = %+v", r)
	}
	// But only in the Session: the list's ctrl+s isn't this one.
	if m.KeyText("session.send") != "ctrl+enter" || !m.Changed("session.send") {
		t.Error(m.KeyText("session.send"))
	}
}

func TestSwap(t *testing.T) {
	m := Build(acts, File{Bindings: map[string][]string{
		"session.send": {"enter"}, "session.enter": {"ctrl+s"},
	}}, nil)
	if p := m.Problems(); len(p) != 0 {
		t.Fatal(p)
	}
	if r := m.Resolve([]Context{Session}, nil, "enter"); r.Key != "ctrl+s" {
		t.Errorf("enter = %+v", r)
	}
	if r := m.Resolve([]Context{Session}, nil, "ctrl+s"); r.Key != "enter" {
		t.Errorf("ctrl+s = %+v", r)
	}
}

func TestChord(t *testing.T) {
	m := Build(acts, File{Bindings: map[string][]string{
		"command:drafts": {"ctrl+g d"},
		"session.stop":   {"ctrl+g x"},
	}}, nil)
	if p := m.Problems(); len(p) != 0 {
		t.Fatal(p)
	}
	r := m.Resolve([]Context{Session}, nil, "ctrl+g")
	if !slices.Equal(r.Pending, Seq{"ctrl+g"}) {
		t.Fatalf("prefix = %+v", r)
	}
	if r2 := m.Resolve([]Context{Session}, r.Pending, "d"); r2.Run != "command:drafts" {
		t.Errorf("chord = %+v", r2)
	}
	if r2 := m.Resolve([]Context{Session}, r.Pending, "x"); r2.Key != "ctrl+x" {
		t.Errorf("chord to default = %+v", r2)
	}
	if r2 := m.Resolve([]Context{Session}, r.Pending, "q"); r2.Missed.String() != "ctrl+g q" {
		t.Errorf("miss = %+v", r2)
	}
	// Commands work from the list too.
	if r := m.Resolve([]Context{List}, nil, "ctrl+g"); len(r.Pending) != 1 {
		t.Errorf("list prefix = %+v", r)
	}
}

func TestChordTakesDefaultPrefix(t *testing.T) {
	// ctrl+x is list.stop's; a chord of yours starting with it takes it.
	m := Build(acts, File{Bindings: map[string][]string{"command:drafts": {"ctrl+x d"}}}, nil)
	if r := m.Resolve([]Context{List}, nil, "ctrl+x"); len(r.Pending) != 1 {
		t.Errorf("= %+v", r)
	}
	if m.KeyText("list.stop") != "" {
		t.Error(m.KeyText("list.stop"))
	}
}

func TestRefused(t *testing.T) {
	m := Build(acts, File{Bindings: map[string][]string{
		"command:drafts": {"d"}, // types
		// Yours are taken in name order: plugin:haven.open takes quit's
		// default, so session.send finds it yours already, and is refused.
		"plugin:haven.open": {"ctrl+q"},
		"session.send":      {"ctrl+q"},
		"nope":              {"ctrl+e"},
	}}, nil)
	got := make([]string, 0, len(m.Problems()))
	for _, p := range m.Problems() {
		got = append(got, p.Action)
	}
	slices.Sort(got)
	if !slices.Equal(got, []string{"command:drafts", "nope", "session.send"}) {
		t.Errorf("problems %v", m.Problems())
	}
}

func TestPluginSuggestionOnlyWhereFree(t *testing.T) {
	m := Build(acts, File{}, map[string][]string{
		"plugin:haven.open": {"ctrl+s", "alt+h"},
		"session.send":      {"alt+z"}, // not the plugin's to suggest
	})
	if got := m.KeyText("plugin:haven.open"); got != "alt+h" {
		t.Errorf("suggested = %q", got)
	}
	if m.KeyText("session.send") != "ctrl+s" {
		t.Error("a plugin moved agtop's key")
	}
}

func TestConflicts(t *testing.T) {
	m := Build(acts, File{}, nil)
	if c := m.Conflicts("command:drafts", Seq{"ctrl+x"}); !slices.Equal(c, []string{"list.stop", "session.stop"}) {
		t.Errorf("%v", c)
	}
	if c := m.Conflicts("session.send", Seq{"ctrl+q"}); !slices.Equal(c, []string{"quit"}) {
		t.Errorf("%v", c)
	}
}

func TestDefaultsAreClean(t *testing.T) {
	m := Build(Defaults, File{}, nil)
	if p := m.Problems(); len(p) != 0 {
		t.Fatal(p)
	}
	seen := map[string]bool{}
	for _, a := range Defaults {
		if seen[a.ID] {
			t.Errorf("%s twice", a.ID)
		}
		seen[a.ID] = true
		for _, k := range a.Keys {
			s, _ := Parse(k)
			if s.String() != k {
				t.Errorf("%s: %q isn't written as agtop reads it (%q)", a.ID, k, s)
			}
		}
	}
}

func TestOwnDefaultKeysArriveAsPressed(t *testing.T) {
	m := Build([]Action{{ID: "place.next", Context: Global, Keys: []string{".", ">", "ctrl+\\"}}}, File{}, nil)
	for _, k := range []string{".", ">", "ctrl+\\"} {
		if r := m.Resolve(nil, nil, k); r.Key != k {
			t.Errorf("%q became %+v", k, r)
		}
	}
}
