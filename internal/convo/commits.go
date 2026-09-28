package convo

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent/tool"
)

// A commit card read from the command and its output is a guess: a message
// given as "$msg" reads as "$msg", a loop's ten commits as one, a quiet one
// as nothing. When the session's folder is on this machine, git says what
// was committed instead. The hashes git printed are the commits, when it
// printed any; otherwise they're what HEAD's reflog took in while the
// command ran.

var (
	cdTo    = regexp.MustCompile(`(?:^|[;&|(\n]\s*)cd\s+("[^"]*"|'[^']*'|\S+)`)
	gitDirC = regexp.MustCompile(`\bgit\s+-C\s+("[^"]*"|'[^']*'|\S+)`)
	// What a reflog calls a commit being made; a rebase's picks and a
	// merge's fast-forward are the merge card's.
	reflogCommit = regexp.MustCompile(`^(commit(?: \((amend|initial|merge)\))?|cherry-pick|revert): `)
	shortstat    = regexp.MustCompile(`(\d+) files? changed(?:, (\d+) insertions?\(\+\))?(?:, (\d+) deletions?\(-\))?`)
)

// commitQuery is what's asked of git for a step: the folders it may have
// committed in, when it ran, and the hashes git printed.
type commitQuery struct {
	dirs     string // \n-separated
	from, to int64  // unix seconds the step ran in, as the reflog counts them
	shas     string // \n-separated, as printed
	amended  bool   // the command amends, for printed commits the reflog misses
}

type commitLookup struct {
	done  bool
	cards []card // nil when git couldn't say
}

// A reflog read after a window closed holds all of that window.
type reflogRead struct {
	at      time.Time
	entries []reflogEntry
}

type reflogEntry struct {
	sha   string
	at    int64
	amend bool
}

var (
	lookups = struct {
		sync.Mutex
		m      map[commitQuery]*commitLookup
		reflog map[string]reflogRead
	}{m: map[commitQuery]*commitLookup{}, reflog: map[string]reflogRead{}}
	// commitsGen counts finished lookups, so what was drawn before one
	// finished is drawn again.
	commitsGen atomic.Int64
	// Only a few lookups at once: a long transcript asks about every
	// commit it made the first time it's drawn.
	lookupSlots = make(chan struct{}, 4)
)

// gitCommits is what git says a finished step committed, and whether it
// could say. It never runs git while a frame is drawn: the first call
// starts the lookup and says no, and a frame after it finishes has it.
func (d *drawer) gitCommits(st *Step) ([]card, bool) {
	q, ok := d.commitQuery(st)
	if !ok {
		return nil, false
	}
	lookups.Lock()
	defer lookups.Unlock()
	l := lookups.m[q]
	if l == nil {
		l = &commitLookup{}
		lookups.m[q] = l
		go func() {
			lookupSlots <- struct{}{}
			cs := lookCommits(q)
			<-lookupSlots
			lookups.Lock()
			l.cards, l.done = cs, true
			lookups.Unlock()
			commitsGen.Add(1)
		}()
	}
	if !l.done || l.cards == nil {
		return nil, false
	}
	return l.cards, true
}

func (d *drawer) commitQuery(st *Step) (commitQuery, bool) {
	if st.kind() != tool.Shell || st.Status == Running || st.Status == Waiting || st.Start.IsZero() || st.End.IsZero() {
		return commitQuery{}, false
	}
	cmd := st.in().Command
	commits := false
	for _, c := range gitCalls(cmd) {
		commits = commits || c.verb == "commit" || c.verb == "cherry-pick" || c.verb == "revert"
	}
	base := firstNonEmpty(d.s.Info.Cwd, d.s.Cwd)
	if !commits || base == "" {
		return commitQuery{}, false
	}
	dirs := []string{base}
	plain := blankHeredocs(cmd)
	for _, re := range []*regexp.Regexp{cdTo, gitDirC} {
		for _, m := range re.FindAllStringSubmatch(plain, -1) {
			dir := unquote(m[1])
			if strings.ContainsAny(dir, "$`") || dir == "-" {
				continue
			}
			if rest, ok := strings.CutPrefix(dir, "~"); ok {
				home, _ := os.UserHomeDir()
				dir = home + rest
			}
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(base, dir)
			}
			if !slices.Contains(dirs, dir) {
				dirs = append(dirs, dir)
			}
		}
	}
	var shas []string
	out := stripANSI(bashOut(st))
	for _, l := range strings.FieldsFunc(out, func(r rune) bool { return r == '\n' || r == '\r' }) {
		if m := commitHead.FindStringSubmatch(l); m != nil {
			shas = append(shas, m[2])
		}
	}
	return commitQuery{
		dirs:    strings.Join(dirs, "\n"),
		from:    st.Start.Unix(),
		to:      st.End.Unix(),
		shas:    strings.Join(shas, "\n"),
		amended: strings.Contains(cmd, "--amend"),
	}, true
}

// lookCommits asks git for q's commits: nil when it can't say, as when the
// folder isn't here or isn't a repository, or nothing turned up.
func lookCommits(q commitQuery) []card {
	var printed []string
	if q.shas != "" {
		printed = strings.Split(q.shas, "\n")
	}
	for _, dir := range strings.Split(q.dirs, "\n") {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		made := windowCommits(dir, q.from, q.to)
		shas := printed
		if len(shas) == 0 {
			// Another session in this folder may commit meanwhile; with
			// nothing printed, what HEAD took in is the best there is.
			for _, e := range made {
				shas = append(shas, e.sha)
			}
		}
		if len(shas) == 0 {
			continue
		}
		cs := showCommits(dir, shas)
		if len(cs) != len(shas) {
			continue // not this folder's, or not all of them
		}
		for i := range cs {
			for _, e := range made {
				if strings.HasPrefix(e.sha, cs[i].sha) || strings.HasPrefix(cs[i].sha, e.sha) {
					cs[i].amend = e.amend
				}
			}
			if len(printed) > 0 && len(made) == 0 {
				cs[i].amend = q.amended // no reflog to say
			}
		}
		return cs
	}
	return nil
}

// windowCommits is each commit HEAD's reflog in dir took in between from
// and to, oldest first.
func windowCommits(dir string, from, to int64) []reflogEntry {
	lookups.Lock()
	r, ok := lookups.reflog[dir]
	lookups.Unlock()
	if !ok || r.at.Unix() <= to {
		at := time.Now()
		raw, err := git(dir, "reflog", "show", "--date=unix", "--format=%H%x1f%gd%x1f%gs", "HEAD")
		if err != nil {
			return nil
		}
		r = reflogRead{at: at}
		for _, l := range strings.Split(string(raw), "\n") {
			f := strings.Split(l, "\x1f")
			if len(f) != 3 {
				continue
			}
			m := reflogCommit.FindStringSubmatch(f[2])
			if m == nil {
				continue
			}
			// HEAD@{1727260000}
			_, ts, _ := strings.Cut(strings.TrimSuffix(f[1], "}"), "@{")
			n, err := strconv.ParseInt(ts, 10, 64)
			if err != nil {
				continue
			}
			r.entries = append(r.entries, reflogEntry{sha: f[0], at: n, amend: m[2] == "amend"})
		}
		lookups.Lock()
		lookups.reflog[dir] = r
		lookups.Unlock()
	}
	var out []reflogEntry
	for _, e := range r.entries {
		if e.at >= from && e.at <= to {
			out = append(out, e)
		}
	}
	slices.Reverse(out) // the reflog is newest first
	return out
}

// showCommits is a card for each of shas, as git has it, in their order.
func showCommits(dir string, shas []string) []card {
	raw, err := git(dir, append([]string{"show", "--shortstat", "--format=%x1e%H%x1f%P%x1f%s%x1f%b%x1f"}, shas...)...)
	if err != nil {
		return nil
	}
	var cs []card
	var full []string
	for _, rec := range strings.Split(string(raw), "\x1e")[1:] {
		f := strings.Split(rec, "\x1f")
		if len(f) != 5 {
			return nil
		}
		c := card{kind: "commit", sha: f[0], subject: f[2], root: f[1] == ""}
		c.body, c.with = messageBody("\n" + strings.TrimRight(f[3], "\n"))
		if s := shortstat.FindStringSubmatch(f[4]); s != nil {
			c.files, _ = strconv.Atoi(s[1])
			c.add, _ = strconv.Atoi(s[2])
			c.del, _ = strconv.Atoi(s[3])
		}
		cs = append(cs, c)
		full = append(full, f[0])
	}
	// The branch each is on: main~2 is main's.
	if names, err := git(dir, append([]string{"name-rev", "--name-only", "--refs=refs/heads/*"}, full...)...); err == nil {
		for i, n := range strings.Split(strings.TrimSpace(string(names)), "\n") {
			if n = strings.TrimSpace(n); i < len(cs) && n != "undefined" {
				cs[i].branch = strings.TrimPrefix(strings.FieldsFunc(n, func(r rune) bool { return r == '~' || r == '^' })[0], "heads/")
			}
		}
	}
	return cs
}

// withCommits is cs with its commit cards swapped for git's, where the
// first of them was, or after any merge's card. The branch git printed
// with a commit is the one it was made on, "detached HEAD" too.
func withCommits(cs, commits []card) []card {
	commits = slices.Clone(commits)
	var out []card
	at := -1
	for _, c := range cs {
		if c.kind == "commit" {
			if at < 0 {
				at = len(out)
			}
			for i := range commits {
				if c.sha != "" && c.branch != "" && strings.HasPrefix(commits[i].sha, c.sha) {
					commits[i].branch = c.branch
				}
			}
			continue
		}
		out = append(out, c)
	}
	if at < 0 {
		for at = 0; at < len(out) && out[at].kind == "merge"; at++ {
		}
	}
	return slices.Concat(out[:at], commits, out[at:])
}
