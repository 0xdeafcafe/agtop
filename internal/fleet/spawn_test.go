package fleet

import (
	"testing"
	"time"

	_ "github.com/0xdeafcafe/agtop/internal/adapters/codex"
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
	host := &Agent{Key: "h", PID: 10}
	l := &Loader{links: map[string]string{}}
	l.foldSpawns(tab, []*Agent{host}, []spawn{{"c", 13}}, parents)
	if host.Subs.Direct != 1 {
		t.Errorf("host's subagents %+v", host.Subs)
	}
}

// A codex exec a Claude Code session's shell ran is that session's while
// it runs, though Codex says no process: found by when its process
// started. Once finished it stays the session's.
func TestSpawnedCodex(t *testing.T) {
	at := time.Now().Add(-time.Minute)
	tab := &proc.Table{Procs: map[int]*proc.Proc{
		11: {PID: 11, PPID: 1, Comm: "claude", Start: at.Add(-time.Hour)},
		12: {PID: 12, PPID: 11, Comm: "zsh", Start: at},
		13: {PID: 13, PPID: 12, Comm: "codex", Start: at.Add(time.Second)},
		20: {PID: 20, PPID: 1, Comm: "codex", Start: at.Add(-time.Hour)}, // codex in a terminal
	}}
	parent := &Agent{Key: "default/i:parent00", PID: 11, Kind: "claude", Interactive: true}
	kid := &Agent{Key: "codex/i:kid00000", Kind: "codex", Interactive: true, Headless: true}
	term := &Agent{Key: "codex/i:term0000", Kind: "codex", Interactive: true}
	kid.CreatedAt, term.CreatedAt = at, at.Add(-time.Hour)
	l := &Loader{links: map[string]string{}}
	got := l.foldSpawns(tab, []*Agent{parent, kid, term}, nil, map[int]bool{11: true})
	if len(got) != 2 || got[0] != parent || got[1] != term {
		t.Fatalf("listed %v", keys(got))
	}
	if parent.Subs.Direct != 1 || parent.Subs.Spawned != 1 {
		t.Errorf("parent's subagents %+v", parent.Subs)
	}
	if l.links[kid.Key] != parent.Key || !l.linksDirty {
		t.Errorf("links %v", l.links)
	}

	// Finished: no process, a past row, folded by the link.
	parent = &Agent{Key: "default/i:parent00", Kind: "claude", Past: true}
	kid = &Agent{Key: "codex/i:kid00000", Kind: "codex", Past: true}
	got = l.foldSpawns(&proc.Table{Procs: map[int]*proc.Proc{}}, []*Agent{parent, kid}, nil, map[int]bool{})
	if len(got) != 1 || got[0] != parent || parent.Subs.Spawned != 1 || parent.Subs.Direct != 0 {
		t.Errorf("listed %v, parent's subagents %+v", keys(got), parent.Subs)
	}
	// One whose session isn't listed stays a row of its own.
	kid = &Agent{Key: "codex/i:kid00000", Kind: "codex", Past: true}
	if got = l.foldSpawns(nil, []*Agent{kid}, nil, map[int]bool{}); len(got) != 1 {
		t.Errorf("an orphan folded away: %v", keys(got))
	}
}

func keys(as []*Agent) []string {
	out := make([]string, 0, len(as))
	for _, a := range as {
		out = append(out, a.Key)
	}
	return out
}
