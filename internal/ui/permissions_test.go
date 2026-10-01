package ui

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/host"
	"github.com/charmbracelet/x/ansi"
)

func TestPermissionPickerPreservesSessionAndDraft(t *testing.T) {
	for _, kind := range []string{"claude", "codex", "ollama", "kimi", "vibe", "gemini"} {
		t.Run(kind, func(t *testing.T) {
			m, c := draftModel()
			c.sess.Info.Kind = kind
			c.sess.Info.Model = "keep-model"
			c.sess.Info.Effort = "high"
			c.sess.Info.PermissionMode = "ask"
			c.sess.Info.PermissionModes = []event.PermissionMode{{ID: "ask", Name: "Ask first"}, {ID: "yolo", Name: "Allow tools"}}
			c.input = []rune("keep my draft")
			c.back = 3
			c.sess.Info.Queue = []string{"queued"}
			before := m.sessionStart(c)
			m.permissionCommand(c, "", false)
			s, ok := m.sheet.(*startSheet)
			if !ok || s.row != 6 {
				t.Fatal("did not open shared permissions row")
			}
			if s.o != before {
				t.Fatalf("opening permissions changed setup: %+v / %+v", s.o, before)
			}
			if got := s.choices(m, 6); !slices.Equal(got, []string{"ask", "yolo"}) {
				t.Fatalf("choices not authoritative: %v", got)
			}
			s.set(m, "yolo")
			after := s.o
			after.mode = before.mode
			if after != before {
				t.Fatal("permission edit altered other setup fields")
			}
			s.key(m, tea.KeyPressMsg{}, "esc")
			if string(c.input) != "keep my draft" || c.back != 3 || c.sess.Info.PermissionMode != "ask" || len(c.sess.Info.Queue) != 1 {
				t.Fatal("picker mutated conversation")
			}
		})
	}
}

func TestPermissionOverrideReachesNewSessionOnly(t *testing.T) {
	m, _ := draftModel()
	original := m.store.Config.Dispatch
	o := m.startDefaults("claude")
	o.mode, o.modeSet = "acceptEdits", true
	if cfg := m.configAs(o, m.startDir()); cfg.PermissionMode != "acceptEdits" {
		t.Fatalf("override lost: %+v", cfg)
	}
	o.mode = ""
	if cfg := m.configAs(o, m.startDir()); cfg.PermissionMode != "" {
		t.Fatal("harness default did not clear override")
	}
	if !reflect.DeepEqual(original, m.store.Config.Dispatch) {
		t.Fatal("per-session permissions changed defaults")
	}
}

func TestPermissionCommandsDoNotInventModesOrOptimisticallyApply(t *testing.T) {
	m, c := draftModel()
	a := m.agentByKey(c.key)
	c.client = &host.Client{}
	c.sess.Info.Kind = "codex"
	c.sess.Info.PermissionMode = "read-only"
	if cmd := m.permissionCommand(c, "", true); cmd == nil {
		t.Fatal("Codex full-access should be available")
	}
	if c.sess.Info.PermissionMode != "read-only" {
		t.Fatal("permissions changed before host acceptance")
	}
	c.sess.Info.PermissionModes = []event.PermissionMode{{ID: "ask"}, {ID: "plan"}}
	if cmd := m.permissionCommand(c, "", true); cmd != nil || !strings.Contains(m.status, "does not advertise") {
		t.Fatal("invented bypass mode")
	}
	for _, text := range []string{"#perm", "#permissions"} {
		m.command(a, text)
		if s, ok := m.sheet.(*startSheet); !ok || s.row != 6 {
			t.Fatalf("%s did not open shared picker", text)
		}
		m.sheet = nil
	}
	if permissionKnown(agent.Kind("codex"), c, "full-access") {
		t.Fatal("static modes overrode runtime choices")
	}
}

func TestPermissionApplyDoesNotSubmitDraftAndFitsSmallSheet(t *testing.T) {
	for _, size := range [][2]int{{44, 24}, {80, 24}, {44, 14}} {
		m, _ := draftModel()
		m.w, m.h = size[0], size[1]
		m.input = []rune("do not send me")
		m.back = 4
		s := &startSheet{o: m.startDefaults("claude"), row: 6, applyOnly: true}
		s.o.mode = "acceptEdits"
		s.o.modeSet = true
		m.sheet = s
		rows := s.body(m, m.w-8, m.h-6)
		text := ansi.Strip(strings.Join(rows, "\n"))
		if !strings.Contains(text, "Permissions") || !strings.Contains(text, "enter Apply") {
			t.Fatalf("%v missing permission controls: %s", size, text)
		}
		if len(rows) > m.h-6 {
			t.Fatal("permission sheet exceeded available height")
		}
		for _, r := range rows {
			if ansi.StringWidth(r) > m.w-8 {
				t.Fatal("permission sheet exceeded available width")
			}
		}
		if cmd := s.key(m, tea.KeyPressMsg{}, "enter"); cmd != nil {
			t.Fatal("applying permissions scheduled a session start")
		}
		if string(m.input) != "do not send me" || m.back != 4 || m.startOver.mode != "acceptEdits" {
			t.Fatal("apply changed draft or lost override")
		}
	}
}

func TestPermissionsAfterHarnessChangeUseDestinationModes(t *testing.T) {
	m, c := draftModel()
	c.sess.Info.Kind = "claude"
	c.sess.Info.PermissionModes = []event.PermissionMode{{ID: "bypassPermissions"}}
	s := &startSheet{o: m.startDefaults("codex"), conn: c.key, row: 6}
	modes := s.choices(m, 6)
	if slices.Contains(modes, "bypassPermissions") || !slices.Contains(modes, "read-only") {
		t.Fatalf("source harness modes leaked: %v", modes)
	}
	if cmd := m.permissionCommand(c, "off", true); cmd != nil {
		t.Fatal("#yolo off must not enable bypass")
	}
}
