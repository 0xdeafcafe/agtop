package ui

import (
	"testing"

	"github.com/0xdeafcafe/agtop/internal/fleet"
)

func TestNeighbourStaysInSection(t *testing.T) {
	m := &Model{}
	sec := func(t string) listLine { return listLine{kind: lineSection, title: t} }
	row := func(k string) listLine { return listLine{kind: lineAgent, agent: &fleet.Agent{Key: k}} }
	m.lines = []listLine{sec("Idle"), row("a"), row("b"), row("c"), {kind: lineBlank},
		sec("Today"), row("d"), {kind: lineBlank}, sec("Done"), row("e")}
	for key, want := range map[string]string{"a": "b", "b": "c", "c": "b", "d": "", "e": "", "x": ""} {
		if got := m.neighbour(key); got != want {
			t.Errorf("neighbour(%q) = %q, want %q", key, got, want)
		}
	}
}
