package ui

import (
	"testing"

	"github.com/0xdeafcafe/rush/internal/convo"
	"github.com/0xdeafcafe/rush/internal/fleet"
	"github.com/0xdeafcafe/rush/internal/proc"
)

// A spawned agent rush hosts counts its whole process tree.
func TestSubUsage(t *testing.T) {
	tab := &proc.Table{
		Procs:    map[int]*proc.Proc{10: {PID: 10, CPU: 20, Footprint: 100}, 11: {PID: 11, CPU: 5, Footprint: 50}},
		Children: map[int][]int{10: {11}},
	}
	m := &Model{snap: &fleet.Snapshot{Table: tab}, order: []*fleet.Agent{{ID: "h1", PID: 10}}}
	c := &hostConn{sess: convo.New(), spawns: map[string]*spawnRun{"r1": {hosted: "h1"}}}
	mem, cpu, n := m.subUsage(c, convo.Subagent{ID: spawnPrefix + "r1"})
	if mem != 150 || cpu != 25 || n != 2 {
		t.Fatalf("got %d bytes, %.0f%%, %d procs", mem, cpu, n)
	}
	if u := usageText(0, 0, 0); u != "" {
		t.Fatalf("no processes, nothing to say: %q", u)
	}
}
