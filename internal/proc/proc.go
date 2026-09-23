package proc

import (
	"sort"
	"time"
)

type Proc struct {
	PID, PPID int
	Comm      string
	Footprint uint64 // bytes; what Activity Monitor calls Memory
	CPUTime   time.Duration
	CPU       float64 // percent of one core since the previous snapshot
	Start     time.Time
}

// Table is one sample of the whole process table.
type Table struct {
	At       time.Time
	Procs    map[int]*Proc
	Children map[int][]int
}

// Snapshot samples every process. prev supplies the CPU-time baseline.
func Snapshot(prev *Table) *Table {
	t := &Table{At: time.Now(), Procs: map[int]*Proc{}, Children: map[int][]int{}}
	for _, p := range list() {
		t.Procs[p.PID] = p
		t.Children[p.PPID] = append(t.Children[p.PPID], p.PID)
	}
	return t.withUsage(prev)
}

// Fill loads memory and CPU for the given roots and all their descendants,
// so a sample never pays for processes nobody is looking at.
func (t *Table) Fill(prev *Table, roots []int) {
	seen := map[int]bool{}
	var walk func(int)
	walk = func(pid int) {
		if seen[pid] {
			return
		}
		seen[pid] = true
		p := t.Procs[pid]
		if p == nil {
			return
		}
		fillUsage(p)
		if prev != nil {
			if old := prev.Procs[pid]; old != nil && old.Start.Equal(p.Start) {
				dt := t.At.Sub(prev.At)
				if dt > 0 && p.CPUTime >= old.CPUTime {
					p.CPU = float64(p.CPUTime-old.CPUTime) / float64(dt) * 100
				}
			}
		}
		for _, c := range t.Children[pid] {
			walk(c)
		}
	}
	for _, r := range roots {
		walk(r)
	}
}

func (t *Table) withUsage(_ *Table) *Table { return t }

// Tree is a process and everything under it, in depth-first order.
func (t *Table) Tree(root int) []TreeNode {
	var out []TreeNode
	var walk func(pid, depth int)
	walk = func(pid, depth int) {
		p := t.Procs[pid]
		if p == nil {
			return
		}
		out = append(out, TreeNode{Proc: p, Depth: depth})
		kids := append([]int(nil), t.Children[pid]...)
		sort.Ints(kids)
		for _, c := range kids {
			walk(c, depth+1)
		}
	}
	walk(root, 0)
	return out
}

type TreeNode struct {
	*Proc
	Depth int
}

// Sum totals memory and CPU over a subtree, not descending into stop pids
// (those are shown as rows of their own).
func (t *Table) Sum(root int, stop map[int]bool) (mem uint64, cpu float64, n int) {
	var walk func(int)
	walk = func(pid int) {
		p := t.Procs[pid]
		if p == nil {
			return
		}
		mem += p.Footprint
		cpu += p.CPU
		n++
		for _, c := range t.Children[pid] {
			if !stop[c] {
				walk(c)
			}
		}
	}
	walk(root)
	return
}

// Descendants returns the subtree's pids, deepest first, for killing.
func (t *Table) Descendants(root int) []int {
	nodes := t.Tree(root)
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Depth > nodes[j].Depth })
	out := make([]int, len(nodes))
	for i, n := range nodes {
		out[i] = n.PID
	}
	return out
}
