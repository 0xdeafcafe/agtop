package ui

import "testing"

// Once the Session is drawn at the pane's width and nothing is left as it
// was, no more frames are asked for: an idle view draws nothing.
func TestRelayoutStopsWhenSettled(t *testing.T) {
	m, _ := benchModel(200, 50)
	for range 3 {
		m.View()
	}
	if m.host == nil || !m.host.drewConvo || m.host.stale {
		t.Skip("no settled Session drawn")
	}
	if m.relayout() != nil {
		t.Fatalf("a settled pane (drawn %d wide) still asks for frames", m.host.paneW)
	}
}
