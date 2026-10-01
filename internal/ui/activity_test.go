package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/convo"
	"github.com/0xdeafcafe/rush/internal/host"
	"github.com/charmbracelet/x/ansi"
)

func TestActivityPinnedWhileScrollingAndFolding(t *testing.T) {
	m, _ := benchModel(200, 45)
	c := m.host
	// Replace the streaming tail with a clear thinking status.
	now := time.Now()
	c.sess.Apply(event.TurnEnd{Reason: "done"}, now)
	c.sess.Apply(host.Sent{Text: "latest turn"}, now)
	c.sess.Apply(event.PartStart{Kind: event.Thinking}, now)
	c.sess.Apply(event.Delta{Kind: event.Thinking, Text: "thinking"}, now)
	for _, width := range []int{40, 100} {
		for _, scroll := range []int{0, 50} {
			c.scroll = scroll
			c.scrollOnly = scroll > 0
			rows := m.rushPane(width, 40)
			if len(rows) > 40 {
				t.Fatalf("pane overflow: %d rows", len(rows))
			}
			pulse := -1
			for i, row := range rows {
				// The tab underline is also a heavy rule.
				if i < c.bodyTop {
					continue
				}
				if strings.Contains(ansi.Strip(row), "━") {
					if pulse >= 0 {
						t.Fatalf("duplicate activity pulse at %d and %d: %s", pulse, i, ansi.Strip(strings.Join(rows, "\n")))
					}
					pulse = i
				}
			}
			if pulse < c.bodyTop || pulse >= c.dockY-m.paneTop {
				t.Fatalf("activity must be above the entire dock: pulse=%d dock=%d", pulse, c.dockY)
			}
			if c.scroll > 0 && pulse != c.dockY-m.paneTop-1 {
				t.Fatalf("scrolled activity must hug the viewport bottom: pulse=%d dock=%d", pulse, c.dockY)
			}
			if !c.sess.Fast {
				t.Fatal("body rendering stopped the dock animation")
			}
			for _, l := range c.shown {
				if strings.Contains(ansi.Strip(l.Text), "━") {
					t.Fatal("scrolling transcript still contains activity")
				}
			}
		}
	}
	c.open["t302"] = false
	if len(m.paneActivity(c, 100)) == 0 {
		t.Fatal("folding latest hid current activity")
	}
	if rows := m.paneDock(m.focused(), c, 100, 40); strings.Contains(ansi.Strip(strings.Join(rows, "\n")), "━") {
		t.Fatal("activity leaked into the task/composer dock")
	}
	c.sess.Apply(event.TurnEnd{Reason: "done"}, now)
	if len(m.paneActivity(c, 100)) > 0 {
		t.Fatal("completed turn left a thinking indicator")
	}
}

func TestActivityOnlyOnChatPage(t *testing.T) {
	m, _ := benchModel(200, 45)
	c := m.host
	c.sess.Apply(event.PartStart{Kind: event.Thinking}, time.Now())
	c.subs = []convo.Subagent{{ID: "child", Type: "test child"}}
	c.subOpen = "child"
	c.subTail = &convo.Tail{Sess: convo.New()}
	c.subTail.Sess.Apply(host.Sent{Text: "child task"}, time.Now())
	c.subTail.Sess.Apply(event.PartStart{Kind: event.Thinking}, time.Now())
	c.subTails = map[string]*convo.Tail{"child": c.subTail}
	for i, name := range m.views(c) {
		c.view = i
		if name == "conversation" {
			if m.activitySession(c) != c.sess || len(m.paneActivity(c, 100)) == 0 {
				t.Fatal("chat lost running indicator")
			}
		} else if m.activitySession(c) != nil || len(m.paneActivity(c, 100)) != 0 {
			t.Fatalf("%s shows the chat running indicator", name)
		}
	}
	c.subOpen = ""
	c.subPeek, c.subPeekID = c.subTail, "child"
	rows := m.subagentLines(c, convo.Options{Width: 180, Now: time.Now(), HideActivity: true})
	for _, row := range rows {
		if strings.Contains(ansi.Strip(row.Text), "━") {
			t.Fatal("subagent preview shows running indicator")
		}
	}
}

func TestActivityFollowsShortConversationAboveQueue(t *testing.T) {
	m, _ := benchModel(200, 45)
	c := m.host
	c.sess = convo.New()
	c.input = nil
	c.sess.Apply(host.Sent{Text: "a short conversation"}, time.Now())
	c.sess.Apply(event.PartStart{Kind: event.Thinking}, time.Now())
	c.sess.Info.Queue = []string{"queued follow-up"}
	rows := m.rushPane(100, 40)
	pulse, queue := -1, -1
	for i, row := range rows {
		text := ansi.Strip(row)
		if i >= c.bodyTop && strings.Contains(text, "━") {
			pulse = i
		}
		if strings.Contains(text, "queued follow-up") {
			queue = i
		}
	}
	if pulse < c.bodyTop || pulse >= c.dockY-m.paneTop || pulse >= queue {
		t.Fatalf("pulse=%d dock=%d queue=%d", pulse, c.dockY, queue)
	}
	if pulse >= c.dockY-m.paneTop-2 {
		t.Fatal("short conversation should keep activity beside its output, leaving spare space below")
	}
}
