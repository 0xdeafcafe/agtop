package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Programmer is an adapter whose agent is a program on this machine: name
// is what it's called, dirs are where its installer puts it besides PATH
// (under the home folder when relative: ".kimi-code/bin").
type Programmer interface {
	Program() (name string, dirs []string)
}

// commonDirs are where agents' installers put their programs, for an
// agtop started with a thin PATH (from the Dock, launchd or a menu bar).
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
// one while agtop runs shows it within this, or at once after Recheck.
const installedFor = 3 * time.Minute

var found struct {
	sync.Mutex
	at    time.Time
	paths map[Kind]string // "" when it isn't installed
}

// look finds every registered agent's program, at most every installedFor.
func look() map[Kind]string {
	found.Lock()
	defer found.Unlock()
	if found.paths != nil && time.Since(found.at) < installedFor {
		return found.paths
	}
	paths := map[Kind]string{}
	for _, a := range All() {
		p, ok := a.(Programmer)
		if !ok {
			paths[a.Kind()] = "-" // nothing to look for: taken as there
			continue
		}
		name, dirs := p.Program()
		paths[a.Kind()], _ = Find(name, dirs...)
	}
	found.paths, found.at = paths, time.Now()
	return paths
}

// Recheck forgets what was found, so the next ask looks again.
func Recheck() {
	found.Lock()
	found.paths = nil
	found.Unlock()
}

// Installed is whether agent k's program is on this machine.
func Installed(k Kind) bool { return look()[k] != "" }

// Path is where agent k's program is; empty when it isn't installed, or
// has no program to find.
func Path(k Kind) string {
	if p := look()[k]; p != "-" {
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
