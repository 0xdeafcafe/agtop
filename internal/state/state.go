// Package state is what agents remembers itself. It never lives inside a
// Claude config dir, so Claude Code updates cannot clobber it and it cannot
// corrupt Claude Code.
package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/0xdeafcafe/agtop/internal/claude"
)

func Dir() string {
	if d := os.Getenv("AGTOP_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "agtop")
}

// Key identifies a job across accounts.
func Key(account, id string) string { return account + "/" + id }

type Config struct {
	Accounts  []claude.Account `json:"accounts"`
	Active    string           `json:"active"`
	GroupBy   string           `json:"groupBy"`
	Folds     map[string]bool  `json:"folds,omitempty"`
	Dispatch  Dispatch         `json:"dispatch"`
	Quiet     bool             `json:"quiet,omitempty"`
	DockLines int              `json:"dockLines,omitempty"`
	// SideWidth is the agent list's share of a split screen, 0.25 to 0.5;
	// zero means agtop's own choice.
	SideWidth float64 `json:"sideWidth,omitempty"`
	SortBy    string  `json:"sortBy,omitempty"`
	Hibernate struct {
		AfterMinutes int `json:"afterMinutes"`
	} `json:"hibernate"`
}

// AllAccounts is the default account plus any configured ones.
// Dispatch is how new sessions start: which coding agent, model, effort and
// permission mode. Empty means Claude Code's own default.
type Dispatch struct {
	Agent      string `json:"agent,omitempty"`
	Model      string `json:"model,omitempty"`
	Effort     string `json:"effort,omitempty"`
	Permission string `json:"permission,omitempty"`
	// RunIn is where new Claude sessions run: "" for agtop mode (agtop's own
	// host, headless) or "daemon" for Claude Code's background service.
	RunIn string `json:"runIn,omitempty"`
	// OnLimit is what agtop-mode sessions do when a usage limit stops them:
	// "" asks once per session (opt-in), "auto" continues at the reset,
	// "off" waits for you.
	OnLimit string `json:"onLimit,omitempty"`
	// Lean starts agtop-mode sessions without Claude Code's non-essential
	// network traffic: ready in about half the time, but without DesignSync,
	// Projects, plugin downloads or live preview.
	Lean bool `json:"lean,omitempty"`
	// RestMinutes is how long an idle agtop-mode session keeps Claude Code
	// running before stopping it (a message starts it again); 0 is the
	// default.
	RestMinutes int `json:"restMinutes,omitempty"`
}

// DefaultRest is how long an idle agtop-mode session keeps Claude Code
// running when RestMinutes isn't set. An idle Claude Code holds 150-580 MB;
// starting it again takes about a second, and the prompt cache (an hour)
// isn't lost.
const DefaultRest = 5 * time.Minute

// Rest is how long an idle agtop-mode session keeps Claude Code running.
func (d Dispatch) Rest() time.Duration {
	if d.RestMinutes > 0 {
		return time.Duration(d.RestMinutes) * time.Minute
	}
	return DefaultRest
}

func (d Dispatch) Flags() []string {
	var f []string
	for _, p := range [][2]string{{"--agent", d.Agent}, {"--model", d.Model}, {"--effort", d.Effort}, {"--permission-mode", d.Permission}} {
		if p[1] != "" {
			f = append(f, p[0], p[1])
		}
	}
	return f
}

func (c Config) AllAccounts() []claude.Account {
	out := []claude.Account{claude.DefaultAccount()}
	for _, a := range c.Accounts {
		if a.ConfigDir == "" || a.ConfigDir == out[0].ConfigDir {
			if a.Name != "" {
				out[0].Name = a.Name
			}
			continue
		}
		out = append(out, a)
	}
	return out
}

func (c Config) ActiveAccount() claude.Account {
	all := c.AllAccounts()
	for _, a := range all {
		if a.Name == c.Active {
			return a
		}
	}
	return all[0]
}

type Overlay struct {
	Done   map[string]time.Time `json:"done,omitempty"`
	Names  map[string]string    `json:"names,omitempty"`
	Groups map[string]string    `json:"groups,omitempty"`
	Moved  map[string]string    `json:"moved,omitempty"`
	Seen   map[string]time.Time `json:"seen,omitempty"`
}

type Store struct {
	mu      sync.Mutex
	Config  Config
	Overlay Overlay
}

func Load() *Store {
	s := &Store{}
	readJSON(filepath.Join(Dir(), "config.json"), &s.Config)
	readJSON(filepath.Join(Dir(), "state.json"), &s.Overlay)
	if s.Overlay.Done == nil {
		s.Overlay.Done = map[string]time.Time{}
	}
	if s.Overlay.Names == nil {
		s.Overlay.Names = map[string]string{}
	}
	if s.Overlay.Groups == nil {
		s.Overlay.Groups = map[string]string{}
	}
	if s.Overlay.Moved == nil {
		s.Overlay.Moved = map[string]string{}
	}
	if s.Overlay.Seen == nil {
		s.Overlay.Seen = map[string]time.Time{}
	}
	return s
}

func (s *Store) SaveOverlay() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSON(filepath.Join(Dir(), "state.json"), s.Overlay)
}

func (s *Store) SaveConfig() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSON(filepath.Join(Dir(), "config.json"), s.Config)
}

func readJSON(path string, v any) {
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, v)
	}
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// CostCache persists transcript totals so a restart does not rescan gigabytes.
const costCacheVersion = 2

type CostCache struct {
	mu      sync.Mutex
	Version int                       `json:"version"`
	Files   map[string]*claude.Totals `json:"files"`
	dirty   bool
}

func cacheDir() string {
	if d := os.Getenv("AGTOP_CACHE"); d != "" {
		return d
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return Dir()
	}
	return filepath.Join(d, "agtop")
}

// CachePath is a file in agtop's cache folder: what can be worked out
// again, but is kept so a restart needn't.
func CachePath(name string) string { return filepath.Join(cacheDir(), name) }

func LoadCostCache() *CostCache {
	c := &CostCache{Files: map[string]*claude.Totals{}}
	readJSON(filepath.Join(cacheDir(), "costs.json"), c)
	if c.Version != costCacheVersion {
		c.Files, c.Version = nil, costCacheVersion
	}
	if c.Files == nil {
		c.Files = map[string]*claude.Totals{}
	}
	// Transcripts Claude Code has since deleted (it keeps them 30 days by
	// default) needn't be remembered.
	for p := range c.Files {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			delete(c.Files, p)
			c.dirty = true
		}
	}
	return c
}

func (c *CostCache) Get(path string) *claude.Totals {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.Files[path]
	if t == nil {
		t = &claude.Totals{}
		c.Files[path] = t
	}
	return t
}

func (c *CostCache) MarkDirty() {
	c.mu.Lock()
	c.dirty = true
	c.mu.Unlock()
}

func (c *CostCache) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.dirty {
		return nil
	}
	c.dirty = false
	// Compact: it's a cache nobody reads, and indenting made it a third bigger.
	path := filepath.Join(cacheDir(), "costs.json")
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path+".tmp", b, 0o600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}
