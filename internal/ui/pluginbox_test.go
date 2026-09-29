package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/rush/internal/hooks"
	"github.com/0xdeafcafe/rush/internal/plugin"
)

// A plugin sets a Session's box whole, chips and cursor and all, and only
// while it holds what the plugin expects; the box it's shown is the same.
func TestPluginSetsTheBoxWhole(t *testing.T) {
	m, _ := benchModel(200, 50)
	c := m.host
	m.paneFocus = true
	c.input, c.back = []rune("typed"), 0
	stale := "not what's there"
	b := &plugin.Box{Text: "see [Image #3] and [Pasted text #2 +4 lines] ok", Cursor: 4,
		Pastes: map[int]string{2: "a\nb\nc\nd"}, Images: map[int]string{3: "/tmp/a.png"}}
	m.pluginDo(plugin.UIDo{Kind: "input.set", Session: c.key, Box: b, If: &stale})
	if string(c.input) != "typed" {
		t.Fatalf("set over what was typed meanwhile: %q", string(c.input))
	}
	was := "typed"
	m.pluginDo(plugin.UIDo{Kind: "input.set", Session: c.key, Box: b, If: &was})
	if string(c.input) != b.Text || len(c.input)-c.back != 4 {
		t.Fatalf("box %q, cursor %d", string(c.input), len(c.input)-c.back)
	}
	text, images := c.imgs.resolve(c.pastes.expand(string(c.input), false))
	if !strings.Contains(text, "a\nb\nc\nd") || len(images) != 1 || images[0] != "/tmp/a.png" {
		t.Fatalf("chips don't stand for what they did: %q %v", text, images)
	}
	if n := c.imgs.add("/tmp/b.png"); n != "[Image #4]" {
		t.Fatalf("the next image should follow the ones set: %s", n)
	}
	got, who, ok := m.boxState()
	if !ok || who != c.key || got.Text != string(c.input) || got.Cursor != 4 || got.Images[3] != "/tmp/a.png" || got.Pastes[2] == "" {
		t.Fatalf("box state %+v %q", got, who)
	}
	if c.undo.undo(c.input, c.back); len(c.undo.future) != 1 {
		t.Fatal("a plugin's set should be one undo away")
	}
}

// A plugin's note sits on the box's bottom edge.
func TestPluginNoteOnTheBox(t *testing.T) {
	m, _ := benchModel(200, 50)
	c := m.host
	m.hooks = hooks.Static(plugin.UIState{Notes: map[string][]plugin.UIStatus{c.key: {{Plugin: "drafts", Status: plugin.Status{Text: "stashed · alt+s brings it back"}}}}})
	if got := ansi.Strip(m.boxNote(c.key)); got != "stashed · alt+s brings it back" {
		t.Fatalf("note %q", got)
	}
	if m.boxNote("") != "" {
		t.Fatal("the Prompt has no note")
	}
}

// A plugin's pick: tabs, a filter, an action that keeps it open without the
// row, and one that closes it.
func TestPluginPick(t *testing.T) {
	m, _ := benchModel(200, 50)
	m.hooks = hooks.Static(plugin.UIState{})
	m.pluginDo(plugin.UIDo{Plugin: "drafts", Kind: "pick", Pick: &plugin.Pick{ID: "h", Title: "Drafts", Tabs: []string{"Sent", "Cleared"},
		Items:   []plugin.PickItem{{ID: "1", Text: "fix the login"}, {ID: "2", Text: "add tests"}, {ID: "3", Tab: 1, Text: "gone"}},
		Actions: []plugin.PickAction{{Key: "enter", Name: "put back"}, {Key: "ctrl+d", Name: "forget", Stay: true}}}})
	s, ok := m.sheet.(*pickSheet)
	if !ok {
		t.Fatal("no pick shown")
	}
	if body := ansi.Strip(strings.Join(s.body(m, 100, 30), "\n")); !strings.Contains(body, "Sent 2") || !strings.Contains(body, "fix the login") || strings.Contains(body, "gone") {
		t.Fatalf("body:\n%s", body)
	}
	pressKeys(m, "t", "e", "s", "t")
	if l := s.shown(); len(l) != 1 || l[0].ID != "2" {
		t.Fatalf("filtered: %+v", l)
	}
	if cmd := s.key(m, mustKey("ctrl+d"), "ctrl+d"); cmd == nil || m.sheet == nil || len(s.shown()) != 0 {
		t.Fatal("forget should tell the plugin and keep the sheet, without the row")
	}
	pressKeys(m, "backspace", "backspace", "backspace", "backspace", "]")
	if s.tab != 1 || len(s.shown()) != 1 {
		t.Fatalf("tab %d: %+v", s.tab, s.shown())
	}
	if cmd := s.key(m, mustKey("enter"), "enter"); cmd == nil || m.sheet != nil {
		t.Fatal("enter should tell the plugin and close the sheet")
	}
}

func mustKey(s string) tea.KeyPressMsg {
	k, ok := keyOf(s)
	if !ok {
		panic("no key " + s)
	}
	return k
}
