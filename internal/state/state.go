// Package state is what agents remembers itself. It never lives inside a
// Claude config dir, so Claude Code updates cannot clobber it and it cannot
// corrupt Claude Code.
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	// Accounts are Claude config folders sessions live in: ~/.claude, and
	// any older ~/.claude-* kept for the sessions it holds. New sessions
	// all start in ~/.claude.
	Accounts []claude.Account `json:"accounts"`
	// Active named the folder new sessions started in, before accounts
	// became logins; cleared once logins are found.
	Active string `json:"active,omitempty"`
	// Logins are the Claude accounts ~/.claude can be signed in as; their
	// sign-ins are in the vault, not here.
	Logins []claude.Login `json:"logins,omitempty"`
	// StayOnAccount keeps agtop from switching ~/.claude to another login
	// when the one in use is nearly out of its 5-hour or weekly usage.
	StayOnAccount bool            `json:"stayOnAccount,omitempty"`
	GroupBy       string          `json:"groupBy"`
	Folds         map[string]bool `json:"folds,omitempty"`
	Dispatch      Dispatch        `json:"dispatch"`
	Quiet         bool            `json:"quiet,omitempty"`
	DockLines     int             `json:"dockLines,omitempty"`
	// SideWidth is the agent list's share of a split screen, 0.25 to 0.5;
	// zero means agtop's own choice.
	SideWidth float64 `json:"sideWidth,omitempty"`
	// View is the layout you picked: "split" (Agents and the Session side
	// by side), "agent" (the Session alone) or "list" (Agents alone).
	// Empty asks, the first time agtop opens.
	View string `json:"view,omitempty"`
	// ListOnly keeps a wide screen to the list alone: no Session beside
	// it until you open one.
	ListOnly bool `json:"listOnly,omitempty"`
	// ChatFull is how a Session opens from Agents alone: the whole screen
	// rather than beside the list. It follows how you last had one.
	ChatFull bool `json:"chatFull,omitempty"`
	// EnterOn is what enter does on an agent in the list: "rename" it, as
	// in the Finder, or "open" it. Empty asks, the first time.
	EnterOn   string `json:"enterOn,omitempty"`
	SortBy    string `json:"sortBy,omitempty"`
	Hibernate struct {
		AfterMinutes int `json:"afterMinutes"`
	} `json:"hibernate"`
	// CleanupHours is how long an agent must have been done and untouched
	// before its worktree (clean and pushed) and temp work are removed on
	// their own; 0 is the default, and a negative number turns it off.
	CleanupHours int `json:"cleanupHours,omitempty"`
	// KeepTranscriptsPlain turns off storing idle transcripts compressed.
	KeepTranscriptsPlain bool `json:"keepTranscriptsPlain,omitempty"`
	// ColorBlind draws added and removed, done and failed in sky blue and
	// amber instead of green and red.
	ColorBlind bool `json:"colorBlind,omitempty"`
	// ShowWhitespace marks spaces and tabs in diffs, as · and →.
	ShowWhitespace bool `json:"showWhitespace,omitempty"`
	// MenuBar keeps agtop's menu bar icon running: usage, what's working,
	// and questions you can answer from their notification.
	MenuBar bool `json:"menuBar,omitempty"`
	// Onboarding is how far a new user has got: the Getting started steps
	// they've done, which one-time tips have shown, and whether they've put
	// Getting started away.
	Onboarding Onboarding `json:"onboarding"`
}

// Onboarding is what agtop has taught you so far.
type Onboarding struct {
	Steps  []string `json:"steps,omitempty"`
	Tips   []string `json:"tips,omitempty"`
	Hidden bool     `json:"hidden,omitempty"`
}

// DefaultCleanup is how long done work waits before it's cleaned up.
const DefaultCleanup = 3 * time.Hour

// SetView keeps layout v ("split", "agent" or "list") for next time.
// Agents alone and the Session alone both leave the list without a Session
// beside it; the Session alone also opens every Session that way.
func (c *Config) SetView(v string) {
	c.View, c.ListOnly, c.ChatFull = v, v != "split", v == "agent"
}

// CleanupAfter is how long done work waits before it's cleaned up; zero
// means never.
func (c Config) CleanupAfter() time.Duration {
	switch {
	case c.CleanupHours < 0:
		return 0
	case c.CleanupHours > 0:
		return time.Duration(c.CleanupHours) * time.Hour
	}
	return DefaultCleanup
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

// ActiveAccount is the folder new sessions start in: always ~/.claude,
// signed in as whichever login is in use.
func (c Config) ActiveAccount() claude.Account { return c.AllAccounts()[0] }

// Vault is where agtop keeps the sign-ins of the logins not in use.
func Vault() claude.Vault { return claude.Vault{Dir: filepath.Join(Dir(), "logins")} }

// SwitchAt is how full, in percent, the login in use may get before agtop
// switches to another.
const SwitchAt = 95.0

// Login is the saved login with id.
func (c Config) Login(id string) (claude.Login, bool) {
	for _, l := range c.Logins {
		if l.ID == id {
			return l, true
		}
	}
	return claude.Login{}, false
}

// NoteLogin records who a login is, adding it named after name (or its
// email) when it's new; it reports whether anything changed.
func (c *Config) NoteLogin(l claude.Login, name string) bool {
	for i, old := range c.Logins {
		if old.ID == l.ID {
			if old.Email == l.Email && old.Org == l.Org && string(old.Profile) == string(l.Profile) {
				return false
			}
			l.Name = old.Name
			c.Logins[i] = l
			return true
		}
	}
	if name == "" {
		name, _, _ = strings.Cut(l.Email, "@")
	}
	if name == "" {
		name = "account"
	}
	l.Name = name
	for n := 2; c.loginNamed(l.Name); n++ {
		l.Name = fmt.Sprintf("%s-%d", name, n)
	}
	c.Logins = append(c.Logins, l)
	return true
}

func (c Config) loginNamed(name string) bool {
	for _, l := range c.Logins {
		if l.Name == name {
			return true
		}
	}
	return false
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
	loadJSON(filepath.Join(Dir(), "config.json"), &s.Config)
	loadJSON(filepath.Join(Dir(), "state.json"), &s.Overlay)
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

// loadJSON reads one of agtop's own files, falling back to the copy of it
// last read whole when it can't be: starting from nothing would save
// nothing over it, and your settings, accounts and done marks with it. The
// unreadable one is kept aside as .broken.
func loadJSON(path string, v any) {
	b, err := os.ReadFile(path)
	if err != nil {
		readJSON(path+".bak", v)
		return
	}
	if json.Valid(b) {
		_ = json.Unmarshal(b, v)
		_ = os.WriteFile(path+".bak", b, 0o600)
		return
	}
	_ = os.WriteFile(path+".broken", b, 0o600)
	readJSON(path+".bak", v)
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	// A temp file of its own, so two agtops saving at once can't write
	// into each other's and leave half of one behind.
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(f.Name(), 0o600)
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return err
	}
	return os.Rename(f.Name(), path)
}

// CostCache persists transcript totals so a restart does not rescan gigabytes.
const costCacheVersion = 3 // 3: the folders each transcript worked in

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
