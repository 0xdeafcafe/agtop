package ui

import (
	"testing"

	"github.com/0xdeafcafe/rush/internal/fleet"
	"github.com/0xdeafcafe/rush/internal/room"
	"github.com/0xdeafcafe/rush/internal/state"
)

func TestRoomRows(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	member := &fleet.Agent{Key: "k1", ID: "s1", Rush: true, DisplayName: "Room · Haiku · poll?"}
	other := &fleet.Agent{Key: "k2", ID: "s2", DisplayName: "other"}
	m := &Model{snap: &fleet.Snapshot{Agents: []*fleet.Agent{member, other}}, store: &state.Store{}, w: 120, h: 40}
	m.rooms.list = []room.Summary{{Room: room.Room{ID: "r1", Topic: "poll?", Members: []room.Member{{Name: "Haiku"}}}, Sessions: map[string]string{"s1": "Haiku"}}}

	rows := m.roomize(m.snap.Agents)
	if len(rows) != 2 || rows[0] != other || rows[1].Key != "room:r1" {
		t.Fatalf("rows: want other then the room, got %v", rows)
	}
	if got := m.roomMembers(rows[1]); len(got) != 1 || got[0] != member {
		t.Fatalf("members under the room: %v", got)
	}
	if _, ok := m.openRoomFor(member); !ok || m.sel != "room:r1" || !m.paneFocus {
		t.Fatalf("a member opens its room: sel %q", m.sel)
	}
	if _, ok := m.openRoomFor(other); ok {
		t.Fatal("an agent outside a room opened one")
	}

	m.askClose(rows[1])
	if m.confirm == nil || m.confirm.onYes == nil || len(m.confirm.more) != 0 {
		t.Fatal("ctrl+x on a room asks only to hide it")
	}
	m.confirm.onYes()
	for _, k := range []string{"room:r1", "k1"} {
		if _, ok := m.store.Overlay.Hidden[k]; !ok {
			t.Errorf("%s not hidden", k)
		}
	}
	if rows := m.roomize([]*fleet.Agent{other}); len(rows) != 1 {
		t.Fatalf("a hidden room still has a row: %v", rows)
	}
}
