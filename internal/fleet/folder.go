package fleet

import (
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Folder is a repository agents work in, as git sees it: the main
// checkout's state and that of each linked worktree an agent is in.
type Folder struct {
	Root string
	Git  GitState
	// Worktrees counts every linked worktree the repository has, whether
	// an agent works in it or not.
	Worktrees int
	// Trees are the linked worktrees agents work in, by path.
	Trees   map[string]GitState
	Checked time.Time
}

// GitState is one checkout's branch and how far it is from committed and
// pushed.
type GitState struct {
	Branch   string
	Upstream bool // the branch tracks a remote one
	Ahead    int  // commits not on the upstream yet
	Behind   int  // commits on the upstream not here yet
	Changed  int  // files with uncommitted changes, untracked ones included
	Err      string
}

// CheckFolder asks git about root and the worktrees under it that agents
// work in. It runs git several times and can take seconds on a big
// checkout: never on the UI's goroutine.
func CheckFolder(root string, trees []string) Folder {
	f := Folder{Root: root, Git: gitState(root), Checked: time.Now()}
	if list, err := git(root, "worktree", "list", "--porcelain"); err == nil {
		for _, l := range strings.Split(list, "\n") {
			if p, ok := strings.CutPrefix(l, "worktree "); ok && p != root {
				f.Worktrees++
			}
		}
	}
	for _, t := range trees {
		if f.Trees == nil {
			f.Trees = map[string]GitState{}
		}
		f.Trees[t] = gitState(t)
	}
	return f
}

// gitState reads a checkout's branch, upstream distance and changes from
// one git status.
func gitState(dir string) GitState {
	out, err := git(dir, "status", "--porcelain=v2", "--branch", "--untracked-files=normal")
	if err != nil {
		return GitState{Err: "git status failed"}
	}
	var s GitState
	for _, l := range strings.Split(out, "\n") {
		switch {
		case l == "":
		case strings.HasPrefix(l, "# branch.head "):
			s.Branch = strings.TrimPrefix(l, "# branch.head ")
			if s.Branch == "(detached)" {
				s.Branch = "detached"
			}
		case strings.HasPrefix(l, "# branch.upstream "):
			s.Upstream = true
		case strings.HasPrefix(l, "# branch.ab "):
			for _, f := range strings.Fields(strings.TrimPrefix(l, "# branch.ab ")) {
				n, _ := strconv.Atoi(f[1:])
				if f[0] == '+' {
					s.Ahead = n
				} else {
					s.Behind = n
				}
			}
		case strings.HasPrefix(l, "#"):
		default:
			s.Changed++
		}
	}
	return s
}

// FolderWants is what CheckFolder should be asked for the agents given:
// each repository they work in, with the linked worktrees they're in.
func FolderWants(agents []*Agent) map[string][]string {
	out := map[string][]string{}
	for _, a := range agents {
		if a.Root == "" {
			continue
		}
		trees := out[a.Root]
		if a.Repo != a.Root && a.Repo != "" && !slices.Contains(trees, a.Repo) {
			trees = append(trees, a.Repo)
		}
		out[a.Root] = trees
	}
	for _, t := range out {
		sort.Strings(t)
	}
	return out
}
