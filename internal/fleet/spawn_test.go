package fleet

import (
	"testing"

	"github.com/0xdeafcafe/agtop/internal/proc"
)

// A claude -p a session's shell ran is that session's: found through the
// shell between them, and counted with its subagents. One a session's
// process runs itself (a host's Claude Code) isn't.
func TestSpawnOf(t *testing.T) {
	tab := &proc.Table{Procs: map[int]*proc.Proc{
		10: {PID: 10, PPID: 1},  // agtop host
		11: {PID: 11, PPID: 10}, // its Claude Code
		12: {PID: 12, PPID: 11}, // zsh -c
		13: {PID: 13, PPID: 12}, // claude -p
		20: {PID: 20, PPID: 1},  // claude -p from a terminal
	}}
	parents := map[int]bool{10: true, 11: true, 13: true, 20: true}
	if got := spawnOf(tab, 13, parents); got != 11 {
		t.Errorf("spawn's parent %d, want 11", got)
	}
	if got := spawnOf(tab, 11, parents); got != 0 {
		t.Errorf("a host's own Claude Code taken for a spawn of %d", got)
	}
	if got := spawnOf(tab, 20, parents); got != 0 {
		t.Errorf("a terminal's claude -p taken for a spawn of %d", got)
	}
	host := &Agent{PID: 10}
	countSpawns(tab, []*Agent{host}, []int{13})
	if host.Subs.Direct != 1 {
		t.Errorf("host's subagents %+v", host.Subs)
	}
}
