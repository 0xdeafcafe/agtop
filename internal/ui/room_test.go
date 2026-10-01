package ui

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/rush/internal/agent/tool"
	"github.com/0xdeafcafe/rush/internal/convo"
	"github.com/0xdeafcafe/rush/internal/fleet"
	"github.com/0xdeafcafe/rush/internal/host"
	"github.com/0xdeafcafe/rush/internal/room"
	"github.com/0xdeafcafe/rush/internal/state"
)

// TestRoomFold: a room's log is drawn by the conversation renderer, as any
// agent's session is.
func TestRoomFold(t *testing.T) {
	r := room.Room{ID: "r1", Topic: "poll or watch?", Members: []room.Member{
		{Name: "Haiku", Config: host.Config{Kind: "claude", Model: "haiku"}},
		{Name: "Luna", Config: host.Config{Kind: "codex", Model: "gpt-6-luna"}},
	}}
	es := []room.Entry{
		{From: "Haiku", Kind: room.Turn, Round: 1},
		{From: "Haiku", Kind: room.Note, Text: "Let me read it."},
		{From: "Haiku", Kind: room.Tool, ID: "c1", Text: "reading", Call: &tool.Call{ID: "c1", Name: "Read", Kind: tool.Read}},
		{From: "Haiku", Kind: room.Tool, ID: "c1", Text: "reading run.go", Call: &tool.Call{ID: "c1", Name: "Read", Kind: tool.Read, Input: tool.Input{Path: "internal/room/run.go"}}},
		{From: "Haiku", Kind: room.Done, ID: "c1"},
		{From: room.User, Kind: room.Say, Text: "@Luna be brief", To: "Luna"},
		{From: "Haiku", Kind: room.Say, Text: "**Switch** to fswait.", Ready: "yes", Round: 1},
		{From: "Luna", Kind: room.Turn, Round: 1},
		{From: room.User, Kind: room.Control, Text: "pause"},
		{From: "Luna", Kind: room.Paused, Text: "Luna was paused mid-turn"},
		{From: "room", Kind: room.Verdict, Text: "Haiku: Switch to fswait.\nLuna: Keep polling for now.", Detail: "converged"},
		{From: "room", Kind: room.End, Text: "converged"},
	}
	// RUSH_ROOM=<id> draws a real room's log instead, to look at.
	if id := os.Getenv("RUSH_ROOM"); id != "" {
		var err error
		if r, err = room.Load(id); err != nil {
			t.Fatal(err)
		}
		es, _, _ = room.Read(id, 0)
	}
	s, f := convo.New(), newRoomFold(r)
	for _, e := range es {
		f.apply(s, e)
	}
	got := ansi.Strip(lineText(s.Render(convo.Options{Width: 100, Open: map[string]bool{}})))
	if os.Getenv("RUSH_ROOM") != "" || testing.Verbose() {
		t.Log("\n" + got)
	}
	if os.Getenv("RUSH_ROOM") != "" {
		return
	}
	for _, want := range []string{"◌ Haiku", "Claude Code · round 1", "Let me read it.", "run.go", "@Luna be brief",
		"Switch to fswait.", "ready to sign", "/pause", "⏸ Luna was paused mid-turn", "◌ Verdict", "Luna: Keep polling for now.", "◌ room  converged"} {
		if !strings.Contains(got, want) {
			t.Errorf("room conversation is missing %q", want)
		}
	}
	if s.Live() != nil {
		t.Error("a finished room still has a turn under way")
	}
	if f.paused {
		t.Error("a finished room still reads as paused")
	}
}

func lineText(ls []convo.Line) string {
	var b strings.Builder
	for _, l := range ls {
		b.WriteString(l.Text + "\n")
	}
	return b.String()
}

// TestRoomPane: the box posts to the room, ctrl+x pauses it, alt+1…9 opens
// a member's own session.
func TestRoomPane(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	r, err := room.Create(room.Room{Topic: "t", Members: []room.Member{{Name: "Haiku"}, {Name: "Luna"}}})
	if err != nil {
		t.Fatal(err)
	}
	member := &fleet.Agent{Key: "k1", ID: "s1", Rush: true, DisplayName: "Room · Luna · t"}
	m := &Model{snap: &fleet.Snapshot{}, store: &state.Store{}, w: 120, h: 40}
	m.rooms.members = map[string][]*fleet.Agent{r.ID: {member}}
	c := &hostConn{key: roomKeyPrefix + r.ID, sess: convo.New(), input: []rune("@Luna be brief")}
	m.roomFeed = &roomFeed{r: r, running: true}
	pause, _ := m.roomPaneKey(c, "ctrl+x")
	for _, cmd := range []tea.Cmd{m.sendRoom(c), pause} {
		if cmd != nil {
			cmd()
		}
	}
	es, _, _ := room.Read(r.ID, 0)
	if len(es) != 2 || es[0].To != "Luna" || es[1].Kind != room.Control || es[1].Text != "pause" {
		t.Errorf("posted %+v; want the message to Luna, then pause", es)
	}
	if len(c.input) != 0 {
		t.Error("the box kept what was sent")
	}
	if _, ok := m.roomPaneKey(c, "alt+2"); !ok || m.sel != "k1" {
		t.Errorf("alt+2 selected %q; want Luna's own session", m.sel)
	}
	if _, ok := m.roomPaneKey(&hostConn{key: "agent"}, "ctrl+x"); ok {
		t.Error("an agent's pane took a room's keys")
	}
}
