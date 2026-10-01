package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/host"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
	"time"
)

func TestMessageNavigationKeepsDraft(t *testing.T) {
	m, c := draftModel()
	m.w, m.h = 100, 30
	c.sess.Apply(host.Sent{Text: "first"}, time.Unix(1, 0))
	c.sess.Apply(host.Sent{Text: "steering"}, time.Unix(1, 0))
	c.input = []rune("unfinished draft")
	m.paneKey(tea.KeyPressMsg{}, "ctrl+home")
	if c.sel != "t1" {
		t.Fatalf("first: %q", c.sel)
	}
	m.paneKey(tea.KeyPressMsg{}, "ctrl+end")
	if c.sel != "t1:u:0" {
		t.Fatalf("latest: %q", c.sel)
	}
	m.paneKey(tea.KeyPressMsg{}, "alt+enter")
	s, ok := m.sheet.(*messageSheet)
	if !ok || s.at != 1 {
		t.Fatal("did not open selected user message")
	}
	s.key(m, tea.KeyPressMsg{}, "[")
	if got := ansi.Strip(strings.Join(s.body(m, 80, 20), "\n")); !strings.Contains(got, "first") {
		t.Fatal(got)
	}
	s.key(m, tea.KeyPressMsg{}, "esc")
	if m.sheet != nil || string(c.input) != "unfinished draft" {
		t.Fatal("viewer consumed draft")
	}
}
func TestMessageViewerFullPasteAndScroll(t *testing.T) {
	m, c := draftModel()
	m.w, m.h = 100, 30
	text := "before\n<pasted_content>\n" + strings.Repeat("line\n", 100) + "last pasted line\n</pasted_content>\nafter"
	c.sess.Apply(host.Sent{Text: text}, time.Unix(1, 0))
	m.openUserMessage(c, "")
	s := m.sheet.(*messageSheet)
	s.body(m, 80, 20)
	s.key(m, tea.KeyPressMsg{}, "end")
	got := ansi.Strip(strings.Join(s.body(m, 80, 20), "\n"))
	if !strings.Contains(got, "last pasted line") || !strings.Contains(got, "after") {
		t.Fatal(got)
	}
	if strings.Contains(strings.Join(s.rows, "\n"), "pasted_content") {
		t.Fatal("paste remained folded")
	}
}
func TestMessageViewerImageRead(t *testing.T) {
	m, c := draftModel()
	m.w, m.h = 100, 30
	c.sess.Apply(event.Message{Role: "user", Parts: []event.Part{{Kind: event.Image, Image: &event.ImageData{Path: "/no/such/image.png"}}}}, time.Unix(1, 0))
	m.openUserMessage(c, "")
	s := m.sheet.(*messageSheet)
	cmd := s.key(m, tea.KeyPressMsg{}, "tab")
	if cmd == nil {
		t.Fatal("preview did not schedule off-UI read")
	}
	cmd()
	got := ansi.Strip(strings.Join(s.body(m, 90, 24), "\n"))
	if !strings.Contains(got, "unavailable") {
		t.Fatal(got)
	}
}

func TestAttachmentLinkSelectsImage(t *testing.T) {
	m, c := draftModel()
	m.w, m.h = 100, 30
	c.sess.Apply(host.Sent{Text: "see [Image #1]", Images: []string{"/missing.png"}}, time.Unix(1, 0))
	cmd, ok := m.messageLink(c, "rush:message/t1/image/1")
	s, isSheet := m.sheet.(*messageSheet)
	if !ok || !isSheet || s.tab != 1 || cmd == nil {
		t.Fatal("image chip did not open image preview")
	}
	cmd()
}
