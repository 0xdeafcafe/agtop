package ui

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestDumpFrames writes whole frames in a few states to $AGTOP_FRAMES,
// digits masked so the clock doesn't show: diff two dumps to check a
// change drew nothing differently.
func TestDumpFrames(t *testing.T) {
	out := os.Getenv("AGTOP_FRAMES")
	if out == "" {
		t.Skip("set AGTOP_FRAMES=<file> to dump frames")
	}
	digits := regexp.MustCompile(`[0-9]+`)
	var b strings.Builder
	frame := func(name string, m *Model) {
		m.Update(hostLinesMsg{key: m.host.key, lines: [][]byte{deltaLine}})
		fmt.Fprintf(&b, "=== %s\n%s\n", name, digits.ReplaceAllString(m.View().Content, "#"))
	}
	for _, sz := range [][2]int{{80, 30}, {120, 40}, {170, 50}, {250, 70}} {
		name := fmt.Sprintf("%dx%d", sz[0], sz[1])
		m, _ := benchModel(sz[0], sz[1])
		frame(name, m)
		m.paneFocus = false
		frame(name+" list focus", m)
		m.input = []rune("#re")
		frame(name+" command", m)
		m.input = []rune(strings.Repeat("a long message that wraps ", 30))
		frame(name+" long input", m)
		m.images = []string{"/tmp/shot.png"}
		frame(name+" images", m)
		m.paneFocus, m.images = true, nil
		m.host.input = nil
		frame(name+" empty box", m)
		m.host.sel = "t300"
		frame(name+" selected", m)
		m.host.view = 1
		frame(name+" overview", m)
		m.host.view = 2
		frame(name+" changes", m)
		m.host.view = 0
		m.host.verbose = true
		frame(name+" verbose", m)
		m.zen = true
		frame(name+" zen", m)
	}
	if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
