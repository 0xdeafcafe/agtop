package ui

import (
	"strings"
	"testing"

	"github.com/0xdeafcafe/rush/internal/plugin"
)

// Reading plugins in Update or View fails the test; handed off, it's fine.
func TestPluginsNeverOnTheUI(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(string), "plugin.Approvals") {
			t.Fatalf("recovered %v, want a breach naming plugin.Approvals", r)
		}
	}()
	done := uiBusy("frame")
	defer done()
	goOff(func() { plugin.Approvals() })
	plugin.Approvals()
}
