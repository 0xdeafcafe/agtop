package ui

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/proc"
)

// What a frame shows but only the disk or the kernel knows (a process's
// words, a repository's worktrees, the commands on disk) is never read
// while it's drawn. known keeps what was read; a frame takes what it has
// and asks for the rest, and after the message it's read off the UI
// goroutine and lands as an applyMsg.

type known[K comparable, V any] struct {
	got     map[K]knownVal[V]
	want    map[K]bool
	reading bool
}

type knownVal[V any] struct {
	v  V
	at time.Time
}

// get is k's value as last read, and whether there's one. One missing, or
// older than fresh (when fresh isn't 0), is asked for again.
func (a *known[K, V]) get(k K, fresh time.Duration) (V, bool) {
	e, ok := a.got[k]
	if ok && (fresh == 0 || time.Since(e.at) < fresh) {
		return e.v, true
	}
	if a.want == nil {
		a.want = map[K]bool{}
	}
	a.want[k] = true
	return e.v, ok
}

// read reads what was asked for with f, one read at a time; then, when
// given, runs on the UI goroutine once they're in.
func (a *known[K, V]) read(f func(K) V, then func(*Model) tea.Cmd) tea.Cmd {
	if len(a.want) == 0 || a.reading {
		return nil
	}
	want := a.want
	a.want, a.reading = nil, true
	return func() tea.Msg {
		out := make(map[K]V, len(want))
		for k := range want {
			out[k] = f(k)
		}
		now := time.Now()
		return applyMsg(func(m *Model) tea.Cmd {
			a.reading = false
			a.put(out, now)
			if then != nil {
				return then(m)
			}
			return nil
		})
	}
}

// put keeps values read at.
func (a *known[K, V]) put(vs map[K]V, at time.Time) {
	if a.got == nil || len(a.got) > 4096 {
		a.got = make(map[K]knownVal[V], len(vs))
	}
	for k, v := range vs {
		a.got[k] = knownVal[V]{v, at}
	}
}

// applyMsg is work done off the UI goroutine, applied on it.
type applyMsg func(*Model) tea.Cmd

func (f applyMsg) applyTo(m *Model) tea.Cmd { return f(m) }

// asks are everything a frame asked for, read after the message.
func (m *Model) asks() tea.Cmd {
	return tea.Batch(m.procWords.read(readProcWords, nil), m.pickTrees.read(readWorktrees, nil),
		m.localCmds.read(readCommands, nil), m.paths.read(readPath, (*Model).pathsLanded))
}

// --- a process's words ---

type procKey struct {
	pid   int
	start int64
}

// shortCmd is a process's command with the home folder and binary paths
// trimmed: its name until its words are read.
func (m *Model) shortCmd(pid int, comm string) string {
	k := procKey{pid: pid}
	if m.snap != nil && m.snap.Table != nil {
		if p := m.snap.Table.Procs[pid]; p != nil {
			k.start = p.Start.UnixNano()
		}
	}
	if cmd, ok := m.procWords.get(k, 0); ok && cmd != "" {
		return cmd
	}
	return comm
}

func readProcWords(k procKey) string {
	args := proc.Args(k.pid)
	if len(args) == 0 {
		return ""
	}
	args[0] = filepath.Base(args[0])
	return strings.TrimRight(ansi.Strip(trimCmd(oneLine(strings.Join(args, " ")), 200)), " ")
}

// --- a repository's worktrees, for the folder picker ---

// worktreesOf are repo's worktrees under .claude/worktrees, as last read.
func (m *Model) worktreesOf(repo string) []string {
	if repo == "" {
		return nil
	}
	list, _ := m.pickTrees.get(repo, 30*time.Second)
	return list
}

func readWorktrees(repo string) []string {
	dir := filepath.Join(repo, ".claude", "worktrees")
	ents, _ := os.ReadDir(dir)
	var list []string
	for _, e := range ents {
		if e.IsDir() {
			list = append(list, filepath.Join(dir, e.Name()))
		}
	}
	return list
}

// --- the commands and skills on disk, for a profile and folder ---

type cmdsKey struct {
	kind agent.Kind
	p    agent.Profile
	cwd  string
}

// commandsOf are the commands and skills agent k has on disk for p in
// cwd, as read at most 30 seconds ago; nil until they're first read.
func (m *Model) commandsOf(k agent.Kind, p agent.Profile, cwd string) []agent.Command {
	cmds, _ := m.localCmds.get(cmdsKey{k, p, cwd}, 30*time.Second)
	return cmds
}

func readCommands(k cmdsKey) []agent.Command {
	if c, ok := agent.As[agent.Commander](k.kind); ok {
		return c.Commands(k.p, k.cwd)
	}
	return nil
}

// --- whether paths are files, for images typed, pasted or dropped ---

// pathFact is what's at a path.
type pathFact struct{ ok, dir bool }

// pathLookup says what's at a path: in the view, m.lookPath, from what was
// read off the UI goroutine.
type pathLookup func(path string) pathFact

func readPath(p string) pathFact {
	st, err := os.Stat(p)
	return pathFact{ok: err == nil, dir: err == nil && st.IsDir()}
}

// lookPath is what was last found at p; nothing, until it's read.
func (m *Model) lookPath(p string) pathFact {
	f, _ := m.paths.get(p, 10*time.Second)
	return f
}

// pathsKnown is whether every path text names has been looked at.
func (m *Model) pathsKnown(text string) bool {
	all := true
	for _, p := range pathCandidates(text) {
		if _, ok := m.paths.get(p, 10*time.Second); !ok {
			all = false
		}
	}
	return all
}

// statPaste looks at the paths a paste names, off the UI goroutine, and
// then has the paste taken in again, knowing them.
func (m *Model) statPaste(msg tea.PasteMsg) tea.Cmd {
	for _, p := range pathCandidates(msg.Content) {
		delete(m.paths.want, p) // read here, not again after the message
	}
	return func() tea.Msg {
		facts := map[string]pathFact{}
		for _, p := range pathCandidates(msg.Content) {
			facts[p] = readPath(p)
		}
		now := time.Now()
		return applyMsg(func(m *Model) tea.Cmd {
			m.paths.put(facts, now)
			return func() tea.Msg { return msg }
		})
	}
}

// pathsLanded takes in images typed as paths whose files were only just
// found: as a space or enter after them would have.
func (m *Model) pathsLanded() tea.Cmd {
	if m.inKind == inPrompt || m.inKind == inReply {
		if in, imgs := pullImages(m.input, m.images, m.lookPath); len(imgs) > len(m.images) {
			m.input, m.images = in, imgs
			m.setCursor(len(in))
		}
	}
	if c := m.host; c != nil {
		if t, ok := c.imgs.inline(string(c.input), m.lookPath); ok {
			c.input = []rune(t)
			c.back = min(c.back, len(c.input))
		}
	}
	return nil
}
