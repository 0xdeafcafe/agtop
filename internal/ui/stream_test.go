package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/community"
	"github.com/0xdeafcafe/rush/internal/state"
	"github.com/charmbracelet/x/ansi"
)

func TestStreamNewestAtBottom(t *testing.T) {
	t0 := time.Now().Add(-time.Hour)
	threads := []community.Thread{
		{ID: "b", Title: "second question", Messages: []community.Message{{Text: "second question", At: t0.Add(2 * time.Minute)}}},
		{ID: "a", Title: "first question", Messages: []community.Message{{Text: "first", At: t0}, {Text: "a late reply", At: t0.Add(5 * time.Minute)}}},
	}
	m := &Model{store: &state.Store{}}
	m.store.Config.Twatter = true
	m.stream.posts = streamOf(threads)
	lines, keys := m.streamLines(60, 40)
	text := ansi.Strip(strings.Join(lines, "\n"))
	first, second, reply := strings.Index(text, "first question"), strings.Index(text, "second question"), strings.Index(text, "a late reply")
	if !(first >= 0 && first < second && second < reply) {
		t.Fatalf("want oldest at the top, newest at the bottom:\n%s", text)
	}
	if !strings.Contains(text, "╭─ Twatter ") || !strings.Contains(text, "╰") {
		t.Fatalf("posts should sit in the Twatter frame:\n%s", text)
	}
	if keys[len(keys)-2] != streamKeyPrefix+"a/1" {
		t.Fatalf("last row should open the late reply, got %q", keys[len(keys)-2])
	}
	// Short on room, the oldest go off the top first.
	lines, _ = m.streamLines(60, 6)
	if text := ansi.Strip(strings.Join(lines, "\n")); !strings.Contains(text, "a late reply") || strings.Contains(text, "first question\n") && strings.Index(text, "first") < strings.Index(text, "second") {
		t.Fatalf("small room should keep the newest:\n%s", text)
	}
}

// The side shows each post on one line, one @, and an agent repeating
// itself once.
func TestStreamOneLineNoRepeats(t *testing.T) {
	t0 := time.Now().Add(-time.Hour)
	me := community.Author{Name: "hub-header-sims"}
	tip := func(id string, at time.Duration) community.Thread {
		return community.Thread{ID: id, Title: "Tip: verify sim env", Author: me, Messages: []community.Message{{Text: "Tip: verify sim env", Author: me, At: t0.Add(at)}}}
	}
	m := &Model{store: &state.Store{}}
	m.store.Config.Twatter = true
	m.stream.posts = streamOf([]community.Thread{tip("a", 0), tip("b", time.Minute)})
	lines, _ := m.streamLines(60, 40)
	text := ansi.Strip(strings.Join(lines, "\n"))
	if strings.Count(text, "Tip: verify sim env") != 1 || strings.Contains(text, "@@") || len(lines) != 4 {
		t.Fatalf("want one framed one-line post with one @:\n%s", text)
	}
}
