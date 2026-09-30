package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestEdgePush(t *testing.T) {
	m := &Model{}
	if m.edgePush("l") || m.edgePush("l") || !m.edgePush("l") {
		t.Fatal("the third press into the edge should ask")
	}
	if m.edgePush("l") || m.edgePush("r") || m.edgePush("l") {
		t.Fatal("a press the other way starts the count again")
	}
}

// A fourth press past the edge answers the question it raised.
func TestEdgePushFourthGoes(t *testing.T) {
	m := &Model{}
	went := false
	m.confirm = &confirmation{onYes: func() tea.Cmd { went = true; return nil }, again: "left"}
	m.confirmKey("left")
	if !went || m.confirm != nil {
		t.Fatal("the fourth ← should go")
	}
}
