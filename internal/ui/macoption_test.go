package ui

import (
	"runtime"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestMacOption(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("mac only")
	}
	if got := macOption(tea.KeyPressMsg{Code: 'ß', Text: "ß"}).String(); got != "alt+s" {
		t.Fatalf("⌥s is %q", got)
	}
	if got := macOption(tea.KeyPressMsg{Code: 's', Text: "s"}).String(); got != "s" {
		t.Fatalf("s is %q", got)
	}
}
