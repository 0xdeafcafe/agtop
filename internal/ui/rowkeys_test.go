package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/host"
	"testing"
	"time"
)

func TestRowKeysAreTheSameForEveryHarness(t *testing.T) {
	for _, kind := range []agent.Kind{"claude", "codex", "ollama", "kimi", "vibe", "gemini"} {
		t.Run(string(kind), func(t *testing.T) {
			m, c := draftModel()
			m.w, m.h = 100, 35
			c.kind = kind
			c.sess.Info.Kind = string(kind)
			c.sess.Apply(host.Sent{Text: "a message"}, time.Now())
			c.sel = "t1"
			c.open = map[string]bool{"t1": false}
			for _, draft := range []string{"", "unfinished /model"} {
				c.input = []rune(draft)
				c.sel = "t1"
				c.open["t1"] = false
				for _, key := range []string{"ctrl+enter", "ctrl+s"} {
					if cmd := m.paneKey(tea.KeyPressMsg{}, key); cmd != nil {
						t.Fatalf("%s dispatched while navigating", key)
					}
					if c.open["t1"] || string(c.input) != draft || c.sel != "t1" {
						t.Fatalf("%s changed row or draft", key)
					}
				}
				m.paneKey(tea.KeyPressMsg{Code: tea.KeyEnter}, "enter")
				if !c.open["t1"] || string(c.input) != draft {
					t.Fatal("Enter should expand without sending or editing the retained draft")
				}
				m.paneKey(tea.KeyPressMsg{Code: tea.KeyEnter}, "enter")
				if c.open["t1"] {
					t.Fatal("Enter should collapse again")
				}
				m.paneKey(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}, "space")
				if !c.open["t1"] || string(c.input) != draft {
					t.Fatal("Space should expand without editing retained draft")
				}
				m.paneKey(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}, "space")
				if c.open["t1"] {
					t.Fatal("Space should collapse again")
				}
				m.paneKey(tea.KeyPressMsg{}, "esc")
				if c.sel != "" || string(c.input) != draft {
					t.Fatal("Esc should return to unchanged composer")
				}
			}
			c.input = []rune("hello")
			c.back = 0
			c.sel = ""
			m.paneKey(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}, "space")
			if string(c.input) != "hello " {
				t.Fatalf("composer Space = %q", string(c.input))
			}
		})
	}
}

func TestTypingFromRowReturnsToComposer(t *testing.T) {
	m, c := draftModel()
	c.sel = "t1"
	c.input = []rune("draft")
	m.paneKey(tea.KeyPressMsg{Code: 'x', Text: "x"}, "x")
	if c.sel != "" || string(c.input) != "draftx" {
		t.Fatalf("selection %q, draft %q", c.sel, string(c.input))
	}
}

func TestQueueNavigationDoesNotReplaceDraft(t *testing.T) {
	m, c := draftModel()
	m.queueLocal(c.key, "queued message")
	c.sel = "q:0"
	c.input = []rune("retained draft")
	if _, used := m.queueKey(c, "space"); !used {
		t.Fatal("selected queue should handle Space")
	}
	if string(c.input) != "retained draft" || c.editQ != 0 {
		t.Fatal("queue navigation overwrote composer draft")
	}
}

func TestExchangeRowSpaceAndEnter(t *testing.T) {
	m, c := draftModel()
	c.sel = "exchange:received:message-1:unknown-peer"
	c.open = map[string]bool{}
	c.input = []rune("retained draft")
	m.paneKey(tea.KeyPressMsg{}, "enter")
	if !c.open[c.sel] || string(c.input) != "retained draft" {
		t.Fatal("Enter should expand the exchange, leaving the draft")
	}
	m.paneKey(tea.KeyPressMsg{}, "enter")
	m.paneKey(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}, "space")
	if !c.open[c.sel] {
		t.Fatal("Space should expand initially closed exchange")
	}
	m.paneKey(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}, "space")
	if c.open[c.sel] {
		t.Fatal("Space should close exchange")
	}
	m.paneKey(tea.KeyPressMsg{}, "right")
	if m.status == "" || string(c.input) != "retained draft" {
		t.Fatal("missing peer must be explained without altering draft")
	}
}

// The row key setting picks enter or space alone; the other one says
// which it is and leaves the row and the draft as they were.
func TestRowKeySetting(t *testing.T) {
	for _, c := range []struct{ set, opens, not string }{{"enter", "enter", "space"}, {"space", "space", "enter"}} {
		m, hc := draftModel()
		m.store.Config.RowKey = c.set
		hc.sess.Apply(host.Sent{Text: "a message"}, time.Now())
		hc.sel, hc.input = "t1", []rune("draft")
		m.paneKey(tea.KeyPressMsg{}, c.not)
		if hc.open["t1"] || string(hc.input) != "draft" || m.status == "" {
			t.Fatalf("%s set: %s should only say what opens the row", c.set, c.not)
		}
		m.paneKey(tea.KeyPressMsg{}, c.opens)
		if !hc.open["t1"] || string(hc.input) != "draft" {
			t.Fatalf("%s set: %s should open the row", c.set, c.opens)
		}
	}
}
