package ui

import (
	"os"
	"path/filepath"
	"testing"

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
	got, ok := r.inline(`before '` + p + `' after`)
	if !ok || got != "before [Image #1] after" || r.Path[1] != p {
		t.Fatalf("inline = %q %v %v", got, ok, r.Path)
	}
}
