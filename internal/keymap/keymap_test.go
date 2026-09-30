package keymap

import (
	"slices"
	"strings"
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
		t.Error("a plugin moved rush's key")
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
				t.Errorf("%s: %q isn't written as rush reads it (%q)", a.ID, k, s)
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

// The Prompt's keys come first while something's typed, and are run; the
// list's own have the same keys back once it's empty, or once you move
// the Prompt's.
func TestPromptBeforeList(t *testing.T) {
	acts := []Action{
		{ID: "list.groupby", Context: List, Keys: []string{"ctrl+s"}},
		{ID: "prompt.stash", Context: Prompt, Keys: []string{"ctrl+s"}, Runs: true},
	}
	m := Build(acts, File{}, nil)
	if p := m.Problems(); len(p) != 0 {
		t.Fatal(p)
	}
	if r := m.Resolve([]Context{Prompt, List}, nil, "ctrl+s"); r.Run != "prompt.stash" {
		t.Errorf("typed = %+v", r)
	}
	if r := m.Resolve([]Context{List}, nil, "ctrl+s"); r.Key != "ctrl+s" || r.Run != "" {
		t.Errorf("empty = %+v", r)
	}
	m = Build(acts, File{Bindings: map[string][]string{"prompt.stash": {"ctrl+x s"}}}, nil)
	if r := m.Resolve([]Context{Prompt, List}, nil, "ctrl+s"); r.Key != "ctrl+s" || r.Run != "" {
		t.Errorf("moved = %+v", r)
	}
	if r := m.Resolve([]Context{Prompt, List}, Seq{"ctrl+x"}, "s"); r.Run != "prompt.stash" {
		t.Errorf("chord = %+v", r)
	}
}

// rush's own keys don't clash, and none needs the option key: every
// action has one without ⌥, and no two share a key (or a chord's first
// key) where both are live. The Prompt's shadow the list's by design.
func TestDefaultsBuildClean(t *testing.T) {
	m := Build(Defaults, File{}, nil)
	if p := m.Problems(); len(p) != 0 {
		t.Fatal(p)
	}
	// The terminal's (ctrl+c, and enter, tab, backspace, esc as ctrl keys)
	// and the boxes' editing keys, taken nowhere a box can have the keys.
	taken := []string{"ctrl+c", "ctrl+m", "ctrl+i", "ctrl+h", "ctrl+[", "ctrl+j", "ctrl+a", "ctrl+e", "ctrl+w", "ctrl+u"}
	for _, a := range Defaults {
		if len(a.Keys) > 0 && !slices.ContainsFunc(a.Keys, func(k string) bool { return !strings.Contains(strings.Fields(k)[0], "alt+") }) {
			t.Errorf("%s has only ⌥ keys: %v", a.ID, a.Keys)
		}
		for _, k := range a.Keys {
			if slices.Contains(taken, strings.Fields(k)[0]) && a.Context != Pages {
				t.Errorf("%s: %s is the terminal's or a box's", a.ID, k)
			}
		}
	}
	for _, c := range Contexts {
		seen := map[string]string{}
		for _, a := range Defaults {
			if !slices.Contains(scope(a.Context), c) && (a.Context != Global || c == Any) {
				continue
			}
			for _, k := range a.Keys {
				if other, ok := seen[k]; ok {
					t.Errorf("%s: %s is both %s and %s", c, k, other, a.ID)
				}
				seen[k] = a.ID
			}
		}
		for k, id := range seen {
			if first, _, chord := strings.Cut(k, " "); chord {
				if other, ok := seen[first]; ok {
					t.Errorf("%s: %s is %s, and %s's chord %s starts with it", c, first, other, id, k)
				}
			}
		}
	}
	if s, p := m.KeyText("session.stash"), m.KeyText("prompt.stash"); s != p {
		t.Errorf("the stash is %s in a Session but %s in the Prompt", s, p)
	}
}

// A default chord arrives as its action's key rush's handling knows: ctrl+]
// then h as ⌥h.
func TestDefaultChordArrivesAsItsKey(t *testing.T) {
	m := Build(Defaults, File{}, nil)
	if r := m.Resolve([]Context{Session}, nil, "ctrl+]"); !slices.Equal(r.Pending, Seq{"ctrl+]"}) {
		t.Fatalf("ctrl+] = %+v", r)
	}
	if r := m.Resolve([]Context{Session}, Seq{"ctrl+]"}, "h"); r.Key != "alt+h" {
		t.Errorf("ctrl+] h = %+v", r)
	}
	if r := m.Resolve([]Context{List}, nil, "ctrl+d"); r.Key != "ctrl+d" {
		t.Errorf("ctrl+d = %+v", r)
	}
}
