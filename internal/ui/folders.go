package ui

import (
	"cmp"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/cellw"
	"github.com/0xdeafcafe/agtop/internal/fleet"
)

// Grouped by folder, the list has a section per repository agents work
// in, its linked worktrees folded in under it, each headed by what git
// says of it. Git is asked in the background, every folderEvery at most.
const folderEvery = 15 * time.Second

// folderCache is what git last said of each folder in the list.
type folderCache struct {
	byRoot  map[string]fleet.Folder
	looking bool
	asked   time.Time
}

type foldersMsg map[string]fleet.Folder

// scratchSection holds agents working in temp folders, which are no one's
// project; noFolder those whose folder isn't known.
const (
	scratchSection = "scratch"
	noFolder       = "No folder"
)

// folderKey is the folder an agent's row sits under: its repository's
// main checkout, or where it works when that isn't a repository.
func folderKey(a *fleet.Agent) string {
	switch {
	case a.Root != "":
		return a.Root
	case strings.Contains(a.Cwd, "/var/folders/") || strings.HasPrefix(a.Cwd, "/tmp/"):
		return scratchSection
	case a.Cwd == "":
		return noFolder
	}
	return a.Cwd
}

// folderTitles names each folder by its last element, or its whole path
// where two folders share one.
func folderTitles(keys map[string]bool) map[string]string {
	byBase := map[string]int{}
	for k := range keys {
		byBase[filepath.Base(k)]++
	}
	out := make(map[string]string, len(keys))
	for k := range keys {
		switch {
		case k == scratchSection || k == noFolder:
			out[k] = k
		case byBase[filepath.Base(k)] > 1 || !filepath.IsAbs(k):
			out[k] = tildify(k)
		default:
			out[k] = filepath.Base(k)
		}
	}
	return out
}

// treeOf is the linked worktree an agent works in, or "" for the main
// checkout and anywhere that isn't a repository.
func treeOf(a *fleet.Agent) string {
	if a.Root != "" && a.Repo != a.Root {
		return a.Repo
	}
	return ""
}

// folderLess orders a folder's rows: the main checkout's first, then each
// worktree's together, by path.
func (m *Model) folderLess(a, b *fleet.Agent) bool {
	if c := cmp.Compare(treeOf(a), treeOf(b)); c != 0 {
		return c < 0
	}
	return m.sortLess(a, b)
}

// folderMeta is a folder section's count of what its agents are doing:
// only what's happening, working first, then what waits on you.
func folderMeta(agents []*fleet.Agent, now time.Time) string {
	var working, waiting, turn int
	var cost float64
	for _, a := range agents {
		cost += a.Spend.Cost
		switch {
		case a.NeedsYou() || a.Waiting() || a.Halted():
			waiting++
		case a.YourTurn(now):
			turn++
		case a.Live() || a.Busy() || a.Checking:
			working++
		}
	}
	var parts []string
	if working > 0 {
		parts = append(parts, paint(cOrange, fmt.Sprintf("%d working", working)))
	}
	if waiting > 0 {
		parts = append(parts, paint(cYellow+bold, fmt.Sprintf("%d need you", waiting)))
	}
	if turn > 0 {
		parts = append(parts, paint(cGreen, fmt.Sprintf("%d your turn", turn)))
	}
	parts = append(parts, dim(sectionMeta(len(agents), cost)))
	return strings.Join(parts, dim(" · "))
}

// gitBits is a checkout's state in a few cells: branch, how far it is
// from its upstream, and how much is uncommitted.
func gitBits(s fleet.GitState) string {
	if s.Err != "" {
		return faint("git failed")
	}
	var parts []string
	if s.Branch != "" {
		b := paint(cSub, s.Branch)
		if s.Ahead > 0 {
			b += " " + paint(cOrange, fmt.Sprintf("↑%d", s.Ahead))
		}
		if s.Behind > 0 {
			b += " " + paint(cYellow, fmt.Sprintf("↓%d", s.Behind))
		}
		parts = append(parts, b)
	}
	if s.Changed > 0 {
		parts = append(parts, dim(fmt.Sprintf("%d changed", s.Changed)))
	} else {
		parts = append(parts, faint("clean"))
	}
	return strings.Join(parts, dim(" · "))
}

// folderGit is what a folder section's header says of the repository.
func (m *Model) folderGit(root string) string {
	f, ok := m.folders.byRoot[root]
	if !ok {
		if filepath.IsAbs(root) && m.rootIsRepo(root) {
			return faint("…")
		}
		return ""
	}
	s := gitBits(f.Git)
	switch f.Worktrees {
	case 0:
	case 1:
		s += dim(" · 1 worktree")
	default:
		s += dim(fmt.Sprintf(" · %d worktrees", f.Worktrees))
	}
	return s
}

func (m *Model) rootIsRepo(root string) bool {
	for _, a := range m.snap.Agents {
		if a.Root == root {
			return true
		}
	}
	return false
}

// folderSectionLine heads a folder: its name, what git says of it, and
// what its agents are doing.
func (m *Model) folderSectionLine(l listLine, w int) string {
	arrow := faint("▾ ")
	if l.folded {
		arrow = faint("▸ ")
	}
	// What its agents are doing comes first; git's say gives way to it
	// when the list is narrow.
	head := arrow + paint(cSub+bold, l.title) + "  " + l.meta
	if g := m.folderGit(l.root); g != "" {
		if room := w - 6 - cellw.String(head) - 5; room >= 12 {
			head += dim("  │  ") + fit(g, room)
		}
	}
	if l.folded {
		room := w - cellw.String(head) - 6
		if room >= 20 && l.peek != "" {
			head += "   " + faint(fit(l.peek, room))
		}
		return "  " + head
	}
	n := max(0, w-6-cellw.String(head)-1)
	return "  " + head + " " + faint(strings.Repeat("─", n))
}

// treeLine heads a linked worktree's rows inside its repository's section.
func (m *Model) treeLine(l listLine, w int) string {
	s := "    " + faint("⎇ ") + paint(cSub, filepath.Base(l.root))
	if st, ok := m.folders.byRoot[l.title].Trees[l.root]; ok {
		s += "  " + gitBits(st)
	}
	return fit(s, w)
}

// refreshFolders asks git about the folders in the list, in the
// background, when grouped by folder and nothing's asked lately; at once
// when a folder has none of git's answers yet.
func (m *Model) refreshFolders() tea.Cmd {
	if m.store.Config.GroupBy != "folder" || m.folders.looking {
		return nil
	}
	var listed []*fleet.Agent
	for _, a := range m.order {
		if m.groupOf[a.Key] != "Earlier" {
			listed = append(listed, a)
		}
	}
	wants := fleet.FolderWants(listed)
	if len(wants) == 0 {
		return nil
	}
	stale := time.Since(m.folders.asked) >= folderEvery
	for root, trees := range wants {
		f, ok := m.folders.byRoot[root]
		if !ok {
			stale = true
			break
		}
		for _, t := range trees {
			if _, ok := f.Trees[t]; !ok {
				stale = true
			}
		}
	}
	if !stale {
		return nil
	}
	m.folders.looking, m.folders.asked = true, time.Now()
	return func() tea.Msg {
		out := make(foldersMsg, len(wants))
		var mu sync.Mutex
		var wg sync.WaitGroup
		gate := make(chan struct{}, 4) // a few gits at once, not one per repo
		for root, trees := range wants {
			wg.Go(func() {
				gate <- struct{}{}
				f := fleet.CheckFolder(root, trees)
				<-gate
				mu.Lock()
				out[root] = f
				mu.Unlock()
			})
		}
		wg.Wait()
		return out
	}
}

func (m *Model) onFolders(msg foldersMsg) {
	m.folders.looking = false
	m.folders.byRoot = msg
}
