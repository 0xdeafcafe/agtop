package convo

import (
	"strings"
	"testing"

	"github.com/0xdeafcafe/rush/internal/host"
)

// A running command rush paused says so on its step.
func TestPausedStep(t *testing.T) {
	s := New()
	s.Apply(host.Sent{Text: "build"}, at(0))
	s.Apply(toolUse("a", "Bash", map[string]any{"command": "make all"}), at(1))
	o := Options{Width: 100, Now: at(5)}
	if out := plain(s.Render(o)); strings.Contains(out, "paused") {
		t.Fatalf("not paused:\n%s", out)
	}
	o.Paused = map[string]bool{"a": true}
	if out := plain(s.Render(o)); !strings.Contains(out, "⏸ paused") {
		t.Errorf("paused:\n%s", out)
	}
}
