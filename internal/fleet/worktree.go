package fleet

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Worktree is a linked git worktree an agent works in, and whether it can
// go without losing anything.
type Worktree struct {
	Path   string
	Repo   string // the main checkout it belongs to
	Branch string
	Agents []string // keys of the agents working in it
	Claude bool     // under a repo's .claude/worktrees: made for an agent
	Size   int64

	Changed  int  // files with uncommitted changes, untracked ones included
	Unpushed int  // commits on no remote
	NoRemote bool // the repo has no remote at all
	Locked   bool
	Err      string
	Checked  time.Time
}

// Safe is a worktree whose every change is committed and on a remote:
// removing it loses nothing but files git ignores (builds, dependencies).
func (w Worktree) Safe() bool {
	return w.Err == "" && !w.Checked.IsZero() && w.Changed == 0 && w.Unpushed == 0 && !w.NoRemote && !w.Locked
}

// Losses says what removing it would throw away, for a confirmation.
func (w Worktree) Losses() string {
	var out []string
	if w.Changed > 0 {
		out = append(out, plural(w.Changed, "file")+" with uncommitted changes")
	}
	if w.NoRemote {
		out = append(out, "every commit (the repo has no remote)")
	} else if w.Unpushed > 0 {
		out = append(out, plural(w.Unpushed, "commit")+" not pushed")
	}
	if w.Locked {
		out = append(out, "a lock someone put on it")
	}
	return strings.Join(out, " · ")
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return strconv.Itoa(n) + " " + what + "s"
}

func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0") // a look must not block the agent's own git
	out, err := cmd.Output()
	return strings.TrimRight(string(out), "\n"), err
}

// workDirs are the folders an agent is known to have worked in.
func (a *Agent) workDirs() []string {
	out := append([]string{}, a.Spend.Dirs...)
	for _, d := range []string{a.WorktreePath, a.Cwd} {
		if d != "" {
			out = append(out, d)
		}
	}
	return out
}

// FindWorktrees lists the linked worktrees of every repository the agents
// work in, each with the agents working in it. Nothing is checked yet.
func FindWorktrees(agents []*Agent) []Worktree {
	repos := map[string]bool{}
	seen := map[string]string{}
	for _, a := range agents {
		for _, d := range a.workDirs() {
			if main := mainCheckout(d, seen); main != "" {
				repos[main] = true
			}
		}
	}
	var out []Worktree
	for repo := range repos {
		list, err := git(repo, "worktree", "list", "--porcelain")
		if err != nil {
			continue
		}
		var w *Worktree
		flush := func() {
			if w != nil && w.Path != repo {
				out = append(out, *w)
			}
			w = nil
		}
		for _, l := range strings.Split(list, "\n") {
			switch {
			case strings.HasPrefix(l, "worktree "):
				flush()
				p := strings.TrimPrefix(l, "worktree ")
				w = &Worktree{Path: p, Repo: repo, Claude: strings.Contains(p, "/.claude/worktrees/")}
			case w == nil:
			case strings.HasPrefix(l, "branch "):
				w.Branch = strings.TrimPrefix(strings.TrimPrefix(l, "branch "), "refs/heads/")
			case l == "locked" || strings.HasPrefix(l, "locked "):
				w.Locked = true
			case l == "prunable" || strings.HasPrefix(l, "prunable "):
				w = nil // its folder is already gone
			}
		}
		flush()
	}
	for i := range out {
		w := &out[i]
		for _, a := range agents {
			for _, d := range a.workDirs() {
				if d == w.Path || strings.HasPrefix(d, w.Path+"/") {
					w.Agents = append(w.Agents, a.Key)
					break
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// Check looks at a worktree with git: what's uncommitted, what's unpushed,
// and how much disk it takes. It can take seconds on a big checkout.
func (w *Worktree) Check() {
	w.Checked, w.Err = time.Now(), ""
	status, err := git(w.Path, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		w.Err = "git status failed"
		return
	}
	w.Changed = 0
	if status != "" {
		w.Changed = strings.Count(status, "\n") + 1
	}
	remotes, _ := git(w.Path, "remote")
	w.NoRemote = strings.TrimSpace(remotes) == ""
	w.Unpushed = 0
	if !w.NoRemote {
		n, err := git(w.Path, "rev-list", "--count", "HEAD", "--not", "--remotes")
		if err != nil {
			w.Err = "couldn't compare with the remote"
			return
		}
		w.Unpushed, _ = strconv.Atoi(n)
	}
	w.Size = DiskUsage([]TempDir{{Path: w.Path}})
}

// RemoveWorktree removes a worktree through git, which also forgets it in
// the main checkout; its branch stays. Unless force is set, git refuses one
// with uncommitted changes, and this refuses one with unpushed commits.
func RemoveWorktree(w Worktree, force bool) error {
	if !force {
		c := w
		c.Check()
		if !c.Safe() {
			return fmt.Errorf("%s isn't safe to remove: %s", filepath.Base(w.Path), firstNonEmpty(c.Losses(), c.Err))
		}
	}
	args := []string{"worktree", "remove", w.Path}
	if force {
		args = []string{"worktree", "remove", "--force", "--force", w.Path}
	}
	if _, err := git(w.Repo, args...); err != nil {
		var ee *exec.ExitError
		if errorsAs(err, &ee) && len(ee.Stderr) > 0 {
			return fmt.Errorf("git: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return err
	}
	return nil
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

func errorsAs(err error, target **exec.ExitError) bool { return errors.As(err, target) }

// mainCheckout finds the main checkout of the repository dir is in, by
// looking for .git upwards: stats only, no git. A linked worktree's .git
// is a file naming <main>/.git/worktrees/<name>. seen remembers answers.
func mainCheckout(dir string, seen map[string]string) string {
	var walked []string
	main := ""
	for d := dir; d != "/" && d != "." && d != ""; d = filepath.Dir(d) {
		if m, ok := seen[d]; ok {
			main = m
			break
		}
		walked = append(walked, d)
		p := filepath.Join(d, ".git")
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if st.IsDir() {
			main = d
			break
		}
		b, _ := os.ReadFile(p)
		g := strings.TrimSpace(strings.TrimPrefix(string(b), "gitdir:"))
		if !filepath.IsAbs(g) {
			g = filepath.Join(d, g)
		}
		if i := strings.Index(g, "/.git/worktrees/"); i >= 0 {
			main = g[:i]
		}
		break
	}
	for _, d := range walked {
		seen[d] = main
	}
	return main
}
