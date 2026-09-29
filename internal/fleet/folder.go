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
	// Trees are the linked worktrees agents work in, by path; checked
	// whole, every linked worktree.
	Trees map[string]GitState

	// Checked whole, for the Overview's Projects page: where it's pushed,
	// its last commits, and every linked worktree in the order git lists
	// them.
	Whole  bool
	Remote string // origin, as host/owner/repo where it looks like one
	Recent []Commit
	Linked []string
	// Checked is when git was asked.
	Checked time.Time
}

// Commit is one commit on a checkout's branch.
type Commit struct {
	Subject string
	At      time.Time
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
// work in; whole, about every worktree it has, and its remote and last
// commits too. It runs git several times and can take seconds on a big
// checkout: never on the UI's goroutine.
func CheckFolder(root string, trees []string, whole bool) Folder {
	f := Folder{Root: root, Git: gitState(root), Whole: whole, Checked: time.Now()}
	if list, err := git(root, "worktree", "list", "--porcelain"); err == nil {
		for l := range strings.SplitSeq(list, "\n") {
			if p, ok := strings.CutPrefix(l, "worktree "); ok && p != root {
				f.Worktrees++
				if whole {
					f.Linked = append(f.Linked, p)
				}
			}
		}
	}
	if whole {
		trees = f.Linked
		if u, err := git(root, "remote", "get-url", "origin"); err == nil {
			f.Remote = shortRemote(u)
		}
		if log, err := git(root, "log", "-3", "--format=%ct%x09%s"); err == nil {
			for l := range strings.SplitSeq(log, "\n") {
				at, subject, ok := strings.Cut(l, "\t")
				if !ok {
					continue
				}
				n, _ := strconv.ParseInt(at, 10, 64)
				f.Recent = append(f.Recent, Commit{Subject: subject, At: time.Unix(n, 0)})
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

// shortRemote names a remote URL the way you'd say it: host/owner/repo,
// from either an https or an ssh URL.
func shortRemote(u string) string {
	u = strings.TrimSuffix(strings.TrimSpace(u), ".git")
	scheme := strings.Contains(u, "://")
	if _, rest, ok := strings.Cut(u, "://"); ok {
		u = rest
	}
	if i := strings.Index(u, "@"); i >= 0 {
		u = u[i+1:]
	}
	host, path, ok := strings.Cut(u, ":")
	switch {
	case !ok:
		return u
	case scheme: // host:port/path
		if _, p, ok := strings.Cut(path, "/"); ok {
			return host + "/" + p
		}
		return host
	}
	return host + "/" + path // scp-like host:path
}

// gitState reads a checkout's branch, upstream distance and changes from
// one git status.
func gitState(dir string) GitState {
	out, err := git(dir, "status", "--porcelain=v2", "--branch", "--untracked-files=normal")
	if err != nil {
		return GitState{Err: "git status failed"}
	}
	var s GitState
	for l := range strings.SplitSeq(out, "\n") {
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
			for f := range strings.FieldsSeq(strings.TrimPrefix(l, "# branch.ab ")) {
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
