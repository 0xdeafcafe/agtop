package ui

import (
	"testing"

	"github.com/0xdeafcafe/rush/internal/convo"
)

func TestLinkAt(t *testing.T) {
	row := "  see " + "\x1b]8;;file:///tmp/a.png\x1b\\\x1b[34m\x1b[4mthe shot\x1b[0m\x1b]8;;\x1b\\" + " and " +
		"\x1b]8;;https://x.dev\x1b\\x.dev\x1b]8;;\x1b\\"
	for col, want := range map[int]string{
		0: "", 5: "", 6: "file:///tmp/a.png", 13: "file:///tmp/a.png", 14: "",
		19: "https://x.dev", 23: "https://x.dev", 24: "",
	} {
		if got := linkAt(row, col); got != want {
			t.Errorf("col %d: got %q, want %q", col, got, want)
		}
	}
	// A markdown link drawn by the conversation is found where it's drawn.
	drawn := convo.Inline("open [it](/tmp/b.pdf) now", "")
	if got := linkAt(drawn, 6); got != "file:///tmp/b.pdf" {
		t.Errorf("drawn link: got %q", got)
	}
}

func TestLinkActs(t *testing.T) {
	names := func(acts []linkAct) (out []string) {
		for _, a := range acts {
			out = append(out, a.label)
		}
		return out
	}
	if got := names(linkActs("file:///tmp/a.PNG")); len(got) != 5 || got[1] != "Open with Preview" {
		t.Errorf("image: %v", got)
	}
	if got := names(linkActs("file:///tmp/a.go")); len(got) != 4 || got[1] != "Quick Look" {
		t.Errorf("code: %v", got)
	}
	if got := names(linkActs("https://x.dev")); len(got) != 2 {
		t.Errorf("web: %v", got)
	}
	// A click on a file, a step's path or a thumbnail, asks what to do.
	m := &Model{}
	if !m.openLinkMenu("file:///work/docs/icon.png") || m.picker == nil || m.picker.title != "/work/docs/icon.png" {
		t.Errorf("menu: %+v", m.picker)
	}
}

// o on a picked step finds its file: in its own rows or its pictures'.
func TestPickedLink(t *testing.T) {
	lk := func(u string) string { return "\x1b]8;;" + u + "\x1b\\x\x1b]8;;\x1b\\" }
	c := &hostConn{sel: "s2", shown: []convo.Line{
		{Text: lk("file:///a.png"), Ref: "s1"},
		{Text: "✓ ◧ read", Ref: "s2"},
		{Text: lk("https://x.dev") + lk("file:///b.png")},
		{Text: lk("file:///c.png"), Ref: "s3"},
	}}
	if got := pickedLink(c); got != "file:///b.png" {
		t.Fatalf("got %q", got)
	}
	c.sel = "s4"
	if got := pickedLink(c); got != "" {
		t.Fatalf("nothing picked: got %q", got)
	}
}
