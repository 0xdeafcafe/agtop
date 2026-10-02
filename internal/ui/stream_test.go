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
	if strings.Count(text, "Tip: verify sim env") != 1 || strings.Contains(text, "@@") || len(lines) != 3 {
		t.Fatalf("want one framed one-line post with one @:\n%s", text)
	}
}

// The Settings row turns Twatter on and off, off by default.
func TestTwatterSettingsRow(t *testing.T) {
	m := &Model{store: &state.Store{}}
	row := func() setting {
		for _, sec := range m.generalSections() {
			for _, r := range sec.rows {
				if r.label == "Twatter" {
					return r
				}
			}
		}
		t.Fatal("no Twatter row in General")
		return setting{}
	}
	if r := row(); r.value != "off" {
		t.Fatalf("default should be off, got %q", r.value)
	}
	row().set("on")
	if !m.store.Config.Twatter || row().value != "on" {
		t.Fatal("the row did not turn Twatter on")
	}
	row().set("off")
	if m.store.Config.Twatter {
		t.Fatal("the row did not turn Twatter off")
	}
}

// The timeline docks on the next-agent box at the list's foot, and the
// list is clipped above it rather than pushing it down.
func TestStreamDocksAboveNextAgent(t *testing.T) {
	m, _ := benchModel(200, 50)
	m.store.Config.Twatter = true
	t0 := time.Now().Add(-time.Hour)
	var threads []community.Thread
	for i := 0; i < 12; i++ {
		title := "post number " + string(rune('a'+i))
		threads = append(threads, community.Thread{ID: title, Title: title, Messages: []community.Message{{Text: title, At: t0.Add(time.Duration(i) * time.Minute)}}})
	}
	m.stream.posts = streamOf(threads)
	rows := strings.Split(ansi.Strip(m.render()), "\n")
	top, bottom, lastAgent, next := -1, -1, -1, -1
	for i, r := range rows {
		switch {
		case strings.Contains(r, "╭─ Twatter "):
			top = i
		case top >= 0 && bottom < 0 && strings.Contains(r, "╰"):
			bottom = i
		case strings.Contains(r, "agent number "):
			lastAgent = i
		}
		if strings.Contains(r, "next agent ") && next < 0 && top >= 0 {
			next = i
		}
	}
	if top < 0 || bottom < 0 {
		t.Fatalf("no docked timeline:\n%s", strings.Join(rows, "\n"))
	}
	if n := bottom - top - 1; n != streamDockPosts {
		t.Fatalf("want %d docked posts, got %d", streamDockPosts, n)
	}
	if !strings.Contains(strings.Join(rows[top:bottom], "\n"), "post number l") || strings.Contains(strings.Join(rows[top:bottom], "\n"), "post number a ") {
		t.Fatalf("the dock should keep the newest posts:\n%s", strings.Join(rows[top:bottom+1], "\n"))
	}
	if lastAgent >= top {
		t.Fatalf("list rows run into the dock: last agent row %d, dock at %d", lastAgent, top)
	}
	if next >= 0 && next > bottom+2 {
		t.Fatalf("the dock should sit on the next-agent box: dock ends %d, box at %d", bottom, next)
	}
	// The list is clipped above it: 30 agents don't fit, so its last row
	// sits right on the frame.
	if top-lastAgent > 2 {
		t.Fatalf("the list should fill to the dock: last agent row %d, dock at %d", lastAgent, top)
	}
	m.store.Config.Twatter = false
	if strings.Contains(ansi.Strip(m.render()), "Twatter") {
		t.Fatal("off, there is no dock")
	}
}
