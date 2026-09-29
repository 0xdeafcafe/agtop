package ui

import (
	"os"
	"path/filepath"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/state"
)

func imageBox(t *testing.T) (*Model, *hostConn) {
	t.Helper()
	a := &fleet.Agent{Key: "k"}
	m := &Model{snap: &fleet.Snapshot{Agents: []*fleet.Agent{a}}, store: &state.Store{}, paneFocus: true}
	c := &hostConn{key: "k", sess: convo.New(), open: map[string]bool{}}
	m.host = c
	return m, c
}

// A pasted image goes in the text as [Image #N], at the cursor.
func TestImageGoesInAtTheCursor(t *testing.T) {
	m, c := imageBox(t)
	c.input, c.back = []rune("look here please"), len(" please")
	m.attachImages([]string{"/a.png"})
	if got := string(c.input); got != "look here [Image #1] please" {
		t.Fatalf("box = %q", got)
	}
	if pos := len(c.input) - c.back; string(c.input[:pos]) != "look here [Image #1]" {
		t.Fatalf("cursor after %q", string(c.input[:pos]))
	}
	if c.imgs.Path[1] != "/a.png" {
		t.Fatalf("image 1 = %q", c.imgs.Path[1])
	}
}

// Backspace at a marker's end, or delete at its start, takes the whole
// marker, and with it the image.
func TestImageMarkerDeletesAsOne(t *testing.T) {
	m, c := imageBox(t)
	c.input = []rune("a ")
	m.attachImages([]string{"/a.png", "/b.png"})
	if got := string(c.input); got != "a [Image #1] [Image #2]" {
		t.Fatalf("box = %q", got)
	}
	m.paneKey(tea.KeyPressMsg{Code: tea.KeyBackspace}, "backspace")
	if got := string(c.input); got != "a [Image #1] " {
		t.Fatalf("after backspace: %q", got)
	}
	c.back = len("[Image #1] ")
	m.paneKey(tea.KeyPressMsg{Code: tea.KeyDelete}, "delete")
	if got := string(c.input); got != "a  " {
		t.Fatalf("after delete: %q", got)
	}
	if text, images := c.imgs.resolve(string(c.input)); len(images) != 0 || text != "a  " {
		t.Fatalf("resolved %q %v", text, images)
	}
}

// Sent, markers are numbered in the order they appear and each names the
// image it stood for; a marker typed for no image stays text.
func TestImageMarkersResolve(t *testing.T) {
	var r imageRefs
	one, two, three := r.add("/one.png"), r.add("/two.png"), r.add("/three.png")
	text := "first " + three + " then " + one + " and [Image #9]"
	_ = two // deleted from the text before sending
	got, images := r.resolve(text)
	if got != "first [Image #1] then [Image #2] and [Image #9]" {
		t.Fatalf("text = %q", got)
	}
	if len(images) != 2 || images[0] != "/three.png" || images[1] != "/one.png" {
		t.Fatalf("images = %v", images)
	}
}

// A dropped or typed path to an image becomes its marker where it was.
func TestImagePathsInline(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "shot one.png")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	var r imageRefs
	got, ok := r.inline(`before '`+p+`' after`, statLook)
	if !ok || got != "before [Image #1] after" || r.Path[1] != p {
		t.Fatalf("inline = %q %v %v", got, ok, r.Path)
	}
}

// An image in the box is drawn by its name: a file's own, a clipboard
// screenshot's the time it was taken. What's sent keeps [Image #N].
func TestImageChipNames(t *testing.T) {
	m, c := imageBox(t)
	shot := filepath.Join(os.TempDir(), "agtop-images", "clipboard-20260929-154203.117.png")
	m.attachImages([]string{"/Users/me/rush-icon-v2-c.png", shot})
	if got := string(c.input); got != "[Image #1] [Image #2]" {
		t.Fatalf("box holds %q", got)
	}
	b := box{w: 80, text: c.input, cursor: len(c.input), anchor: -1}.named(c.imgs)
	if got := string(b.text); got != "[▣ rush-icon-v2-c.png] [▣ screenshot 15:42]" {
		t.Fatalf("drawn %q", got)
	}
	if b.cursor != len(b.text) || b.textPos(b.cursor) != len(c.input) {
		t.Fatalf("cursor %d", b.cursor)
	}
	text, images := c.imgs.resolve(string(c.input))
	if text != "[Image #1] [Image #2]" || len(images) != 2 || images[1] != shot {
		t.Fatalf("sent %q %v", text, images)
	}
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "delete":
		return tea.KeyPressMsg{Code: tea.KeyDelete}
	}
	if r, n := utf8.DecodeRuneInString(s); n == len(s) {
		return tea.KeyPressMsg{Code: r, Text: s}
	}
	return tea.KeyPressMsg{}
}

// An arrow onto a chip selects it; the next goes past it. Typing replaces
// it, backspace takes it whole, and undo brings it back with its image.
func TestImageChipIsOneThing(t *testing.T) {
	m, c := imageBox(t)
	c.input = []rune("a ")
	m.attachImages([]string{"/a.png"})
	c.input = append(c.input, 'b')
	chip := seg{2, 2 + len("[Image #1]")}
	press := func(s string) { m.paneKey(key(s), s) }
	sel := func() (int, int) { return c.anchor - 1, len(c.input) - c.back }

	c.back = len(c.input) - chip.from
	press("right")
	if a, p := sel(); a != chip.from || p != chip.to {
		t.Fatalf("right onto it: anchor %d cursor %d", a, p)
	}
	press("right")
	if a, p := sel(); a != -1 || p != chip.to {
		t.Fatalf("right past it: anchor %d cursor %d", a, p)
	}
	press("left")
	if a, p := sel(); a != chip.to || p != chip.from {
		t.Fatalf("left onto it: anchor %d cursor %d", a, p)
	}
	// Drawn, the selection covers the whole name.
	b := box{w: 80, text: c.input, cursor: len(c.input) - c.back, anchor: c.anchor - 1}.named(c.imgs)
	if got := string(b.text[b.cursor:b.anchor]); got != "[▣ a.png]" {
		t.Fatalf("selected %q", got)
	}
	press("x")
	if got := string(c.input); got != "a xb" {
		t.Fatalf("typing over it: %q", got)
	}
	press("super+z")
	if got := string(c.input); got != "a [Image #1]b" {
		t.Fatalf("undo: %q", got)
	}
	// Word jumps and shift never stop inside it.
	c.back, c.anchor = len(c.input), 0
	press("alt+f")
	press("alt+f")
	if _, p := sel(); p != chip.to {
		t.Fatalf("word jump stopped at %d", p)
	}
	press("shift+left")
	if a, p := sel(); a != chip.to || p != chip.from {
		t.Fatalf("shift+left: anchor %d cursor %d", a, p)
	}
	press("backspace")
	if got := string(c.input); got != "a b" {
		t.Fatalf("backspace on it: %q", got)
	}
	if _, images := c.imgs.resolve(string(c.input)); len(images) != 0 {
		t.Fatalf("image still attached: %v", images)
	}
	press("ctrl+/")
	if text, images := c.imgs.resolve(string(c.input)); text != "a [Image #1]b" || len(images) != 1 || images[0] != "/a.png" {
		t.Fatalf("undo: %q %v", text, images)
	}
	// Right before it, delete takes it whole too.
	c.back, c.anchor = len(c.input)-chip.from, 0
	press("delete")
	if got := string(c.input); got != "a b" {
		t.Fatalf("delete before it: %q", got)
	}
}

// A click on an image's name selects it.
func TestClickSelectsAnImage(t *testing.T) {
	m, c := imageBox(t)
	m.listW, m.mode = 40, modeList
	c.input = []rune("hi ")
	m.attachImages([]string{"/pics/cat.png"})
	c.box, c.boxY = box{w: 60, text: c.input, cursor: len(c.input), anchor: -1, lead: "❯ "}.named(c.imgs), 20
	// x0 is listW+3; then "│ ", the lead, "hi " and into "[▣ cat.png]".
	if !m.clickBox(43+2+2+3+5, 21) {
		t.Fatal("the click missed the box")
	}
	if a, p := c.anchor-1, len(c.input)-c.back; a != 3 || p != len(c.input) || m.boxDrag != 0 {
		t.Fatalf("anchor %d cursor %d drag %d", a, p, m.boxDrag)
	}
}

// The Prompt's paste chips are one thing too, and it has undo.
func TestPromptChipAndUndo(t *testing.T) {
	m := &Model{snap: &fleet.Snapshot{}, store: &state.Store{}, mode: modeList}
	chip := m.pastes.add("one\ntwo")
	m.input = []rune("see " + chip)
	press := func(s string) { m.listKey(key(s), s) }
	press("left")
	if a, p := m.anchor-1, m.cursorPos(); a != len(m.input) || p != 4 {
		t.Fatalf("left onto it: anchor %d cursor %d", a, p)
	}
	press("backspace")
	if got := string(m.input); got != "see " {
		t.Fatalf("backspace: %q", got)
	}
	press("super+z")
	if got := string(m.input); got != "see "+chip || m.pastes.expand(string(m.input), false) != "see one\ntwo" {
		t.Fatalf("undo: %q", got)
	}
}
