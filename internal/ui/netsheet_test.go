package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/rush/internal/fleet"
	"github.com/0xdeafcafe/rush/internal/state"
)

func TestNetworkSheet(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := &Model{store: &state.Store{}, w: 140, h: 50, snap: &fleet.Snapshot{}}
	m.command(nil, "#net")
	if _, ok := m.sheet.(*netSheet); !ok {
		t.Fatalf("#net opened %T", m.sheet)
	}
	text := ansi.Strip(strings.Join(m.sheet.body(m, 92, 40), "\n"))
	for _, want := range []string{"API", "Network", "New network", "rush's jobs that use it", "Sessions waiting on it", "Keys and frames"} {
		if !strings.Contains(text, want) {
			t.Fatalf("no %q in:\n%s", want, text)
		}
	}
	t.Log("\n" + text)
}

func TestNetRate(t *testing.T) {
	for b, want := range map[float64]string{0: "0B", 900: "900B", 40 << 10: "40K", 1.5 * (1 << 20): "1.5M", 50 << 20: "50M"} {
		if got := netRate(b); got != want {
			t.Errorf("%v: %q, want %q", b, got, want)
		}
	}
}
