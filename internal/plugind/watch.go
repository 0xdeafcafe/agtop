package plugind

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/0xdeafcafe/agtop/internal/host"
)

// watchEvery is how often a watching plugin's list is checked for changes.
const watchEvery = 2 * time.Second

// watchList answers sessions.watch: the list as it is now, then a
// session.changed notification whenever one appears or changes, and
// session.gone when one is removed. Watching again replaces the old watch.
func (r *runner) watchList() (any, error) {
	now := map[string]Session{}
	out := []Session{}
	// One lister for the watch, so each look reads only the info files that
	// changed.
	l := new(host.Lister)
	for _, i := range l.List() {
		s := sessionOf(i)
		now[s.ID] = s
		out = append(out, s)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.mu.Lock()
	if r.unwatch != nil {
		r.unwatch()
	}
	r.unwatch = cancel
	conn := r.conn
	r.mu.Unlock()
	if conn == nil {
		// Asked for during initialize: the plugin asks again once it's up.
		cancel()
		return nil, errors.New("not ready: watch once initialize has been answered")
	}
	go func() {
		t := time.NewTicker(watchEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-conn.Done():
				return
			case <-t.C:
			}
			seen := map[string]bool{}
			for _, i := range l.List() {
				s := sessionOf(i)
				seen[s.ID] = true
				if was, ok := now[s.ID]; !ok || !same(was, s) {
					now[s.ID] = s
					_ = conn.Notify("session.changed", s)
				}
			}
			for id := range now {
				if !seen[id] {
					delete(now, id)
					_ = conn.Notify("session.gone", map[string]any{"id": id})
				}
			}
		}
	}()
	return out, nil
}

// same says two looks at a session found nothing worth telling a plugin:
// only the time of the look differs.
func same(a, b Session) bool {
	a.UpdatedAt, b.UpdatedAt = time.Time{}, time.Time{}
	return reflect.DeepEqual(a, b)
}

// gitInfo is the checkout a folder is in.
type gitInfo struct {
	repo, branch string
	worktree     bool // a linked worktree, not the main checkout
	at           time.Time
}

var gitCache = struct {
	sync.Mutex
	m map[string]gitInfo
}{m: map[string]gitInfo{}}

// gitOf finds the checkout dir is in by reading .git and HEAD, not by
// running git: it's asked for every session every few seconds.
func gitOf(dir string) gitInfo {
	if dir == "" {
		return gitInfo{}
	}
	now := time.Now()
	gitCache.Lock()
	g, ok := gitCache.m[dir]
	gitCache.Unlock()
	if ok && now.Sub(g.at) < 30*time.Second {
		return g
	}
	g = gitInfo{at: now}
	for d := dir; d != "/" && d != "."; d = filepath.Dir(d) {
		p := filepath.Join(d, ".git")
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		gitDir := p
		if !st.IsDir() {
			b, _ := os.ReadFile(p)
			s := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(b)), "gitdir:"))
			if !filepath.IsAbs(s) {
				s = filepath.Join(d, s)
			}
			gitDir = s
			g.worktree = strings.Contains(filepath.ToSlash(s), "/.git/worktrees/")
		}
		g.repo = d
		if b, err := os.ReadFile(filepath.Join(gitDir, "HEAD")); err == nil {
			h := strings.TrimSpace(string(b))
			g.branch = strings.TrimPrefix(h, "ref: refs/heads/")
			if len(g.branch) == 40 {
				g.branch = g.branch[:8]
			}
		}
		break
	}
	gitCache.Lock()
	if len(gitCache.m) > 512 {
		clear(gitCache.m)
	}
	gitCache.m[dir] = g
	gitCache.Unlock()
	return g
}
