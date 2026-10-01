package acp

import (
	"encoding/json/jsontext"
	"github.com/0xdeafcafe/rush/internal/agent/event"
	"testing"
)

func TestPermissionModesKeepProtocolIDs(t *testing.T) {
	s := &Session{modes: []Mode{{ID: "urn:modes:ask", Name: "Ask", Description: "ask first"}}, mode: "urn:modes:ask"}
	modes := s.permissionModes()
	if len(modes) != 1 || modes[0].ID != s.mode || modes[0].Name != "Ask" {
		t.Fatalf("%+v", modes)
	}
	s.modes = nil
	s.setConfig([]configOption{{ID: "approval", Category: "mode", CurrentValue: jsontext.Value(`"ask"`), Options: []configValue{{Name: "Presets", Options: []configValue{{Value: "ask", Name: "Ask first"}, {Value: "yolo", Name: "Allow tools"}}}}}})
	modes = s.permissionModes()
	if len(modes) != 2 || modes[1].ID != "yolo" || s.mode != "ask" {
		t.Fatalf("%+v / %s", modes, s.mode)
	}
}

func TestInitReportsRuntimePermissionChoices(t *testing.T) {
	s, _ := start(t)
	got := nextOf[event.Init](t, s)
	if got.ModeID != "default" || len(got.Modes) == 0 {
		t.Fatalf("runtime permission options missing: %+v", got)
	}
}
