package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/0xdeafcafe/rush/internal/community"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
	"time"
)

func communityApply(t *testing.T, m *Model, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg, ok := cmd().(sheetMsg)
	if !ok {
		t.Fatal("expected async board result")
	}
	return msg.apply(m)
}
func TestCommunityQuestionReplyResolveAndDraftSafety(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	m, _ := benchModel(100, 35)
	communityApply(t, m, m.openCommunity(""))
	s := m.community
	s.key(m, tea.KeyPressMsg{}, "n")
	s.paste("Why is this slow?\nHere is the context.")
	communityApply(t, m, s.key(m, tea.KeyPressMsg{}, "enter"))
	if s.composing || s.busy || s.thread() == nil {
		t.Fatal("question was not saved")
	}
	thread := s.thread()
	if thread.Title != "Why is this slow?" || len(thread.Messages) != 1 {
		t.Fatalf("bad question: %+v", thread)
	}
	s.key(m, tea.KeyPressMsg{}, "tab")
	s.paste("Try the bounded reader.")
	// Leaving a reply does not send or lose it, and Enter on a selected row is inert.
	s.key(m, tea.KeyPressMsg{}, "esc")
	if cmd := s.key(m, tea.KeyPressMsg{}, "enter"); cmd != nil {
		t.Fatal("navigation Enter submitted draft")
	}
	s.key(m, tea.KeyPressMsg{}, "esc")
	s.key(m, tea.KeyPressMsg{}, "n")
	if s.composing || s.replyTo != thread.ID {
		t.Fatal("new question stole reply draft")
	}
	s.key(m, tea.KeyPressMsg{}, "space")
	s.key(m, tea.KeyPressMsg{}, "tab")
	communityApply(t, m, s.key(m, tea.KeyPressMsg{}, "enter"))
	if len(s.thread().Messages) != 2 {
		t.Fatal("reply not persisted")
	}
	communityApply(t, m, s.key(m, tea.KeyPressMsg{}, "d"))
	if !s.thread().Resolved {
		t.Fatal("resolve not persisted")
	}
	communityApply(t, m, s.key(m, tea.KeyPressMsg{}, "d"))
	if s.thread().Resolved {
		t.Fatal("reopen not persisted")
	}
	list, err := community.List()
	if err != nil || len(list) != 1 || len(list[0].Messages) != 2 {
		t.Fatalf("reload: %+v %v", list, err)
	}
}
func TestCommunityLateRefreshCannotReplacePostedQuestion(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	m, _ := benchModel(100, 35)
	cmd := m.openCommunity("new")
	old := cmd().(sheetMsg)
	s := m.community
	s.paste("A new question")
	communityApply(t, m, s.save(m, false))
	id := s.selected
	old.apply(m)
	if s.selected != id || s.thread() == nil {
		t.Fatal("stale load overwrote new post")
	}
}
func TestCommunityRenderAndPasteIsolation(t *testing.T) {
	for _, size := range [][2]int{{44, 24}, {80, 30}, {140, 45}} {
		m, _ := benchModel(size[0], size[1])
		now := time.Now()
		s := &communitySheet{loaded: true, threads: []community.Thread{{ID: "abc", Title: strings.Repeat("Long title ", 20), Author: community.Author{Name: "Worker", Kind: "codex"}, UpdatedAt: now, Messages: []community.Message{{Author: community.Author{Name: "Worker", Kind: "codex"}, Text: "question\x1b[2J\x07\n" + strings.Repeat("full answer ", 200), At: now}}}}}
		m.community, m.sheet = s, s
		for _, selected := range []string{"", "abc"} {
			s.selected = selected
			lines := s.body(m, size[0]-10, size[1]-6)
			if len(lines) > size[1]-6 {
				t.Fatalf("%v clips controls", size)
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > size[0]-10 {
					t.Fatalf("width overflow: %q", line)
				}
			}
			if strings.Contains(strings.Join(lines, "\n"), "\x1b[2J") {
				t.Fatal("terminal sequence passed through")
			}
		}
		before := string(m.input)
		m.Update(tea.PasteMsg{Content: "must not reach session"})
		if string(m.input) != before || len(s.input) != 0 {
			t.Fatal("paste leaked out of board")
		}
		s.composing = true
		m.Update(tea.PasteMsg{Content: "board draft"})
		if string(s.input) != "board draft" || string(m.input) != before {
			t.Fatal("board paste not isolated")
		}
	}
}

func TestCommunityRefreshKeepsDraftAndSelection(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	first, err := community.Ask(community.Author{Name: "Claude", Kind: "claude"}, "First question", "Details")
	if err != nil {
		t.Fatal(err)
	}
	m, _ := benchModel(100, 35)
	communityApply(t, m, m.openCommunity(first.ID))
	s := m.community
	s.key(m, tea.KeyPressMsg{}, "tab")
	s.paste("My unfinished reply")
	_, err = community.Reply(first.ID, community.Author{Name: "Kimi", Kind: "kimi"}, "A new answer")
	if err != nil {
		t.Fatal(err)
	}
	communityApply(t, m, s.load(m))
	if s.selected != first.ID || string(s.input) != "My unfinished reply" || !s.composing || len(s.thread().Messages) != 2 {
		t.Fatal("refresh lost draft, selection or external answer")
	}
	// An unchanged poll must not replace the live snapshot or regenerate content.
	before := &s.threads[0]
	communityApply(t, m, s.load(m))
	if &s.threads[0] != before {
		t.Fatal("unchanged board was decoded again")
	}
}
