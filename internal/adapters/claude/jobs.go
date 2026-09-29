package claude

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/claude"
)

// memos are what was read from each file, kept until it changes.
var memos = struct {
	sync.Mutex
	by map[string]memoEntry
}{by: map[string]memoEntry{}}

type memoEntry struct {
	mod  time.Time
	size int64
	v    any
}

// memo is read's result for path, read again only when the file has
// changed (or appeared, or gone).
func memo[T any](path string, read func() T) T {
	var mod time.Time
	size := int64(-1)
	if st, err := os.Stat(path); err == nil {
		mod, size = st.ModTime(), st.Size()
	}
	memos.Lock()
	defer memos.Unlock()
	if e, ok := memos.by[path]; ok && e.size == size && e.mod.Equal(mod) {
		return e.v.(T)
	}
	v := read()
	memos.by[path] = memoEntry{mod: mod, size: size, v: v}
	return v
}

// Workers are the daemon's workers, from its roster.
func (Adapter) Workers(p agent.Profile) map[string]int {
	acct := Account(p)
	return memo(acct.RosterPath(), func() map[string]int {
		out := map[string]int{}
		for id, w := range claude.ReadRoster(acct).Workers {
			out[id] = w.PID
		}
		return out
	})
}

// Pins are the native view's pin list.
func (Adapter) Pins(p agent.Profile) map[string]int {
	acct := Account(p)
	return memo(claude.PinsPath(acct), func() map[string]int {
		pins := map[string]int{}
		for i, id := range claude.ReadPins(acct) {
			pins[id] = i + 1
		}
		return pins
	})
}

// LinkedPRs are the PRs Claude Code's status cache has.
func (Adapter) LinkedPRs(p agent.Profile) map[string]agent.PR {
	acct := Account(p)
	return memo(acct.PRCachePath(), func() map[string]agent.PR { return claude.ReadPRCache(acct) })
}

// Watched are the folders sessions and jobs register in, the daemon's
// roster, the PR cache, the pins and the state file, and each job's
// folder.
func (Adapter) Watched(p agent.Profile, jobs []string) []string {
	acct := Account(p)
	out := []string{
		acct.ConfigDir, filepath.Join(acct.ConfigDir, "sessions"), acct.JobsDir(), filepath.Dir(acct.RosterPath()),
		acct.RosterPath(), acct.PRCachePath(), claude.PinsPath(acct), acct.StatePath(),
	}
	for _, id := range jobs {
		out = append(out, filepath.Join(acct.JobsDir(), id), filepath.Join(acct.JobsDir(), id, "state.json"))
	}
	return out
}

// FindTranscript looks for a session's transcript where entering a
// worktree may have moved it.
func (Adapter) FindTranscript(p agent.Profile, cwd, sid string) string {
	return Account(p).FindTranscript(cwd, sid)
}

var (
	_ agent.JobKeeper   = Adapter{}
	_ agent.Transcripts = Adapter{}
)
