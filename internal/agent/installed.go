package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Programmer is an adapter whose agent is a program on this machine: name
// is what it's called, dirs are where its installer puts it besides PATH
// (under the home folder when relative: ".kimi-code/bin").
type Programmer interface {
	Program() (name string, dirs []string)
}

// Lesser is a Programmer whose agent is there, doing less, when only
// another program is: Copilot's coding agent needs only gh. hint says
// what installing the agent's own program adds.
type Lesser interface {
	Lesser() (name string, dirs []string, hint string)
}

// Rider is an adapter that runs on another agent's program: Ollama's
// runs Claude Code. Its own Program is only what else it needs, so it's
// installed when both are, and a shell running that program isn't it.
type Rider interface {
	Rides() Kind
}

// commonDirs are where agents' installers put their programs, for an
// rush started with a thin PATH (from the Dock, launchd or a menu bar).
var commonDirs = []string{
	".local/bin", "bin", ".npm-global/bin", ".bun/bin", ".cargo/bin", "go/bin", ".volta/bin",
	"/opt/homebrew/bin", "/usr/local/bin",
}

// Find is where program name is: on PATH, or in one of dirs or the usual
// install places. Relative dirs are under the home folder.
func Find(name string, dirs ...string) (string, bool) {
	if p, err := exec.LookPath(name); err == nil {
		return p, true
	}
	home, _ := os.UserHomeDir()
	for _, d := range append(append([]string(nil), dirs...), commonDirs...) {
		if !filepath.IsAbs(d) {
			if home == "" {
				continue
			}
			d = filepath.Join(home, d)
		}
		p := filepath.Join(d, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p, true
		}
	}
	return "", false
}

// installedFor is how long a look for an agent's program holds: installing
// one while rush runs shows it within this, or at once after Recheck.
const installedFor = 3 * time.Minute

var found struct {
	sync.Mutex
	at      time.Time
	paths   map[Kind]string    // "" when it isn't installed, "~…" when only its Lesser program is
	homes   map[Kind][]Profile // each agent's Profiles, as last read (only when never waiting)
	noWait  bool               // see NeverWait
	looking bool
}

// NeverWait has every ask answered from what was last found, for rush's
// view, whose UI goroutine never waits on the disk: a stale answer (or none
// yet) has the programs looked for again in the background, and the next
// ask has them. It starts the first look at once.
func NeverWait() {
	found.Lock()
	found.noWait = true
	found.Unlock()
	look()
}

// look finds every registered agent's program, at most every installedFor.
//
//uiblock:nowait the view turns NeverWait on: a goroutine looks
func look() map[Kind]string {
	found.Lock()
	defer found.Unlock()
	if found.paths != nil && time.Since(found.at) < installedFor {
		return found.paths
	}
	if found.noWait {
		if !found.looking {
			found.looking = true
			go func() {
				paths := lookAll()
				found.Lock()
				found.paths, found.at = paths, time.Now()
				found.Unlock()
				// An agent's Profiles asks whether it's installed: read
				// them with what was just found.
				homes := map[Kind][]Profile{}
				for _, a := range All() {
					homes[a.Kind()] = a.Profiles()
				}
				found.Lock()
				found.homes, found.looking = homes, false
				found.Unlock()
			}()
		}
		return found.paths // nil reads as nothing installed, for a moment
	}
	found.paths, found.at = lookAll(), time.Now()
	return found.paths
}

func lookAll() map[Kind]string {
	paths := map[Kind]string{}
	for _, a := range All() {
		p, ok := a.(Programmer)
		if !ok {
			paths[a.Kind()] = "-" // nothing to look for: taken as there
			continue
		}
		name, dirs := p.Program()
		paths[a.Kind()], _ = Find(name, dirs...)
		if l, ok := a.(Lesser); ok && paths[a.Kind()] == "" {
			name, dirs, _ := l.Lesser()
			if p, ok := Find(name, dirs...); ok {
				paths[a.Kind()] = "~" + p
			}
		}
	}
	for _, a := range All() {
		if r, ok := a.(Rider); ok {
			if p := paths[r.Rides()]; p == "" || strings.HasPrefix(p, "~") {
				paths[a.Kind()] = ""
			}
		}
	}
	return paths
}

// Recheck forgets what was found, so the next ask looks again. Never
// waiting, what was found stands until the new look lands.
func Recheck() {
	found.Lock()
	if found.noWait {
		found.at = time.Time{}
	} else {
		found.paths = nil
	}
	found.Unlock()
}

// ProfilesOf is a's Profiles. Never waiting, they're as last read, with
// the programs, in the background.
//
//uiblock:nowait the view turns NeverWait on: a goroutine reads them
func ProfilesOf(a Adapter) []Profile {
	found.Lock()
	noWait := found.noWait
	found.Unlock()
	if !noWait {
		return a.Profiles()
	}
	look() // looks again when it's time
	found.Lock()
	defer found.Unlock()
	return found.homes[a.Kind()]
}

// Installed is whether agent k is on this machine: its program, or the
// lesser one it can do something with.
func Installed(k Kind) bool { return look()[k] != "" }

// Runs is whether agent k's own program is on this machine, so rush can
// run its sessions.
func Runs(k Kind) bool {
	p := look()[k]
	return p != "" && !strings.HasPrefix(p, "~")
}

// Hint is what installing agent k's own program would add, when only its
// lesser one is here; empty otherwise.
func Hint(k Kind) string {
	if !strings.HasPrefix(look()[k], "~") {
		return ""
	}
	a, _ := Get(k)
	_, _, hint := a.(Lesser).Lesser()
	return hint
}

// Path is where agent k's program is; empty when it isn't installed, or
// has no program to find.
func Path(k Kind) string {
	if p := look()[k]; p != "-" && !strings.HasPrefix(p, "~") {
		return p
	}
	return ""
}

// InstalledAll is every registered agent whose program is here, by kind.
func InstalledAll() []Adapter {
	paths := look()
	var out []Adapter
	for _, a := range All() {
		if paths[a.Kind()] != "" {
			out = append(out, a)
		}
	}
	return out
}

// ProgramKind is the agent whose program a shell runs as name (a bare name
// or a path to it), when one is registered.
func ProgramKind(name string) (Kind, bool) {
	k, ok := programKinds()[filepath.Base(name)]
	return k, ok
}

// programs are the registered agents by program name, made again when one
// is registered: ProgramKind is asked of every shell step a session ran.
var programs atomic.Pointer[map[string]Kind]

func programKinds() map[string]Kind {
	if p := programs.Load(); p != nil {
		return *p
	}
	m := map[string]Kind{}
	all := All()
	for i := len(all) - 1; i >= 0; i-- { // the first by kind wins, as it did
		a := all[i]
		if _, ok := a.(Rider); ok {
			continue
		}
		if p, ok := a.(Programmer); ok {
			if n, _ := p.Program(); n != "" {
				m[n] = a.Kind()
			}
		}
	}
	programs.Store(&m)
	return m
}
