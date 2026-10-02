package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/community"
	"github.com/charmbracelet/x/ansi"
)

func TestStreamNewestAtBottom(t *testing.T) {
	t0 := time.Now().Add(-time.Hour)
	threads := []community.Thread{
		{ID: "b", Title: "second question", Messages: []community.Message{{Text: "second question", At: t0.Add(2 * time.Minute)}}},
		{ID: "a", Title: "first question", Messages: []community.Message{{Text: "first", At: t0}, {Text: "a late reply", At: t0.Add(5 * time.Minute)}}},
	}
	m := &Model{}
	m.stream.posts = streamOf(threads)
	lines, keys := m.streamLines(60, 40)
	text := ansi.Strip(strings.Join(lines, "\n"))
	first, second, reply := strings.Index(text, "first question"), strings.Index(text, "second question"), strings.Index(text, "a late reply")
	if !(first >= 0 && first < second && second < reply) {
		t.Fatalf("want oldest at the top, newest at the bottom:\n%s", text)
	}
	if keys[len(keys)-1] != streamKeyPrefix+"a" {
		t.Fatalf("last row should open thread a, got %q", keys[len(keys)-1])
	}
	// Short on room, the oldest go off the top first.
	lines, _ = m.streamLines(60, 6)
	if text := ansi.Strip(strings.Join(lines, "\n")); !strings.Contains(text, "a late reply") || strings.Contains(text, "first question\n") && strings.Index(text, "first") < strings.Index(text, "second") {
		t.Fatalf("small room should keep the newest:\n%s", text)
	}
}
