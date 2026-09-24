package fleet

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/host"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// TempDir is a folder of an agent's scratch work, and whether cleaning it
// empties it (a folder Claude Code expects to find) or removes it.
type TempDir struct {
	Path string
	Keep bool
}

// TempDirs are where an agent's temp work is: what it cloned, built or
// downloaded as scratch, which Claude Code leaves behind when it finishes.
// A Claude Code background job has its own tmp folder; an agtop session
// has one agtop gives it; and every session has Claude Code's own scratch
// (task output and the like) under /tmp/claude-<uid>.
func (a *Agent) TempDirs() []TempDir {
	var out []TempDir
	switch {
	case a.Agtop:
		out = append(out, TempDir{Path: host.TempDir(a.ID), Keep: true})
	case !a.Interactive && !a.Past && a.ID != "":
		out = append(out, TempDir{Path: filepath.Join(a.Acct.JobsDir(), a.ID, "tmp"), Keep: true})
	}
	if a.SessionID != "" && a.Cwd != "" {
		out = append(out, TempDir{Path: filepath.Join(ClaudeScratch(), claude.ProjectSlug(a.Cwd), a.SessionID)})
	}
	return out
}

// ClaudeScratch is where Claude Code keeps per-session scratch.
func ClaudeScratch() string { return filepath.Join("/tmp", fmt.Sprintf("claude-%d", os.Getuid())) }

// DiskUsage is how much disk the folders take, counted as du does
// (allocated blocks), without following links.
func DiskUsage(dirs []TempDir) int64 {
	var n int64
	for _, d := range dirs {
		var st unix.Stat_t
		if unix.Lstat(d.Path, &st) != nil {
			continue
		}
		n += st.Blocks * 512
		if st.Mode&unix.S_IFMT == unix.S_IFDIR {
			n += dirUsage(d.Path)
		}
	}
	return n
}

// dirUsage is what's inside a folder, each entry looked at through the
// folder's own descriptor: a temp folder can hold a node_modules or two, and
// a whole path and a FileInfo for every file made walks allocate megabytes.
func dirUsage(dir string) int64 {
	f, err := os.Open(dir)
	if err != nil {
		return 0
	}
	defer f.Close()
	names, _ := f.Readdirnames(-1)
	fd := int(f.Fd())
	var n int64
	for _, name := range names {
		var st unix.Stat_t
		if unix.Fstatat(fd, name, &st, unix.AT_SYMLINK_NOFOLLOW) != nil {
			continue
		}
		n += st.Blocks * 512
		if st.Mode&unix.S_IFMT == unix.S_IFDIR {
			n += dirUsage(dir + "/" + name)
		}
	}
	return n
}

// CleanTemp deletes an agent's temp work. It refuses while the agent has a
// process, since whatever it's running may be using it.
func CleanTemp(a *Agent) error {
	if a.PID != 0 {
		return fmt.Errorf("%s is still running; stop it first", a.DisplayName)
	}
	for _, d := range a.TempDirs() {
		// Only ever a tmp folder of agtop's or Claude Code's, never
		// something a bad id could point elsewhere.
		if filepath.Base(d.Path) != "tmp" && filepath.Dir(filepath.Dir(d.Path)) != ClaudeScratch() {
			return fmt.Errorf("won't delete %s", d.Path)
		}
		if !d.Keep {
			if err := os.RemoveAll(d.Path); err != nil {
				return err
			}
			continue
		}
		ents, err := os.ReadDir(d.Path)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if err := os.RemoveAll(filepath.Join(d.Path, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// TempSize is an agent's temp work as last measured.
type TempSize struct {
	Bytes int64     `json:"bytes"`
	At    time.Time `json:"at"`
}

// TempSizes remembers what each agent's temp work measured, across
// restarts, so the folders are walked again only when an agent has done
// something since.
type TempSizes struct {
	Sizes map[string]TempSize `json:"sizes"`
	dirty bool
}

func tempPath() string { return state.CachePath("temp.json") }

// LoadTempSizes reads the last measurements.
func LoadTempSizes() *TempSizes {
	t := &TempSizes{}
	if b, err := os.ReadFile(tempPath()); err == nil {
		_ = json.Unmarshal(b, t)
	}
	if t.Sizes == nil {
		t.Sizes = map[string]TempSize{}
	}
	return t
}

// Due are the agents whose temp work should be measured: never measured,
// busy since, or running and not measured for a minute.
func (t *TempSizes) Due(agents []*Agent, now time.Time) []*Agent {
	var out []*Agent
	for _, a := range agents {
		e, ok := t.Sizes[a.Key]
		switch {
		case !ok:
		case a.PID != 0 && now.Sub(e.At) > time.Minute:
		case a.UpdatedAt.After(e.At) && a.PID == 0:
		default:
			continue
		}
		out = append(out, a)
	}
	return out
}

// Set records measurements and saves them.
func (t *TempSizes) Set(m map[string]TempSize) {
	for k, v := range m {
		t.Sizes[k] = v
	}
	t.dirty = true
	t.Save()
}

// Save writes the measurements if they changed.
func (t *TempSizes) Save() {
	if !t.dirty {
		return
	}
	if b, err := json.Marshal(t); err == nil {
		_ = os.MkdirAll(filepath.Dir(tempPath()), 0o700)
		if os.WriteFile(tempPath()+".tmp", b, 0o600) == nil {
			_ = os.Rename(tempPath()+".tmp", tempPath())
		}
	}
	t.dirty = false
}
