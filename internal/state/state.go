// Package state is what agents remembers itself. It never lives inside a
// Claude config dir, so Claude Code updates cannot clobber it and it cannot
// corrupt Claude Code.
package state

import (
	"bufio"
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/jsonx"
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
	// Folders are the Claude config folders an older agtop was given:
	// ~/.claude and ~/.claude-*. Everything is in ~/.claude now; the others
	// are only read to take their sign-ins and past sessions in, once, and
	// dropped after. ~/.claude's entry stays: its name is what its
	// sessions, names and hosted agents are kept under.
	Folders []claude.Account `json:"accounts,omitempty"`
	// FoldersImported is set once the older folders' sign-ins were taken
	// in as logins, so one you forget isn't taken in again.
	FoldersImported bool `json:"foldersImported,omitzero"`
	// Active named the folder new sessions started in, before accounts
	// became logins; cleared once logins are found.
	Active string `json:"active,omitempty"`
	// Logins are the Claude accounts ~/.claude can be signed in as; their
	// sign-ins are in the vault, not here.
	Logins []claude.Login `json:"logins,omitempty"`
	// SignIns are the accounts of agents other than Claude Code that agtop
	// keeps, several to an agent; their credentials are in the vault or
	// the agent's own keeping, not here.
	SignIns []SignIn `json:"signIns,omitempty"`
	// Using is the account an agent runs on where agtop picks it rather
	// than the agent's home saying: a SignIn's ID, by kind.
	Using map[string]string `json:"using,omitempty"`
	// Profiles are the named lists of providers sessions run, with what
	// happens when they run out; DefaultProfile names the one a session
	// gets when neither you nor a FolderRule picked one. The Default
	// profile is made once from DefaultAgent, SwitchOnLimit and
	// AgentOrder, which are still written from it for older agtops.
	Profiles       []Profile    `json:"profiles,omitempty"`
	DefaultProfile string       `json:"defaultProfile,omitempty"`
	FolderRules    []FolderRule `json:"folderRules,omitempty"`
	// SwitchOnLimit is what agtop does when the account in use is nearly out:
	// "" (or "account") switches to another account of the same agent, and
	// sessions carry on; "agent" does that, then starts new sessions on
	// the next agent in AgentOrder once every account of it is out; "off"
	// stays.
	SwitchOnLimit string `json:"switchOnLimit,omitempty"`
	// AgentOrder is the agents new sessions move on to, in turn, when
	// SwitchOnLimit is "agent". Installed agents not in it come after, by name.
	AgentOrder []string `json:"agentOrder,omitempty"`
	// StayOnAccount is SwitchOnLimit "off" as agtops before it read it:
	// kept in step with it.
	StayOnAccount bool            `json:"stayOnAccount,omitzero"`
	GroupBy       string          `json:"groupBy"`
	Folds         map[string]bool `json:"folds,omitempty"`
	Dispatch      Dispatch        `json:"dispatch"`
	Quiet         bool            `json:"quiet,omitzero"`
	DockLines     int             `json:"dockLines,omitzero"`
	// SideWidth is the agent list's share of a split screen, 0.25 to 0.5;
	// zero means agtop's own choice.
	SideWidth float64 `json:"sideWidth,omitzero"`
	// View is the layout you picked: "split" (Agents and the Session side
	// by side), "agent" (the Session alone) or "list" (Agents alone).
	// Empty asks, the first time agtop opens.
	View string `json:"view,omitempty"`
	// ListOnly keeps a wide screen to the list alone: no Session beside
	// it until you open one.
	ListOnly bool `json:"listOnly,omitzero"`
	// ChatFull is how a Session opens from Agents alone: the whole screen
	// rather than beside the list. It follows how you last had one.
	ChatFull bool `json:"chatFull,omitzero"`
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
	CleanupHours int `json:"cleanupHours,omitzero"`
	// KeepTranscriptsPlain turns off storing idle transcripts compressed.
	KeepTranscriptsPlain bool `json:"keepTranscriptsPlain,omitzero"`
	// ColorBlind draws added and removed, done and failed in sky blue and
	// amber instead of green and red.
	ColorBlind bool `json:"colorBlind,omitzero"`
	// Theme is what agtop's colours are made for: "dark" or "light", or,
	// empty, whatever the terminal says its background and text are.
	Theme string `json:"theme,omitempty"`
	// ShowWhitespace marks spaces and tabs in diffs, as · and →.
	ShowWhitespace bool `json:"showWhitespace,omitzero"`
	// SearchTranscriptsOnKey keeps ctrl+k to agent names and commands while
	// you type; ctrl+enter (or ctrl+j) then searches the transcripts.
	SearchTranscriptsOnKey bool `json:"searchTranscriptsOnKey,omitzero"`
	// MenuBar keeps agtop's menu bar icon running: usage, what's working,
	// and questions you can answer from their notification.
	MenuBar bool `json:"menuBar,omitzero"`
	// MenuBarAsked is set once agtop has offered the menu bar icon.
	MenuBarAsked bool `json:"menuBarAsked,omitzero"`
	// Onboarding is how far a new user has got: the Getting started steps
	// they've done, which one-time tips have shown, and whether they've put
	// Getting started away.
	Onboarding Onboarding `json:"onboarding"`
	// Advisor lets agtop's advisor look over your agents' figures now and
	// then, on Haiku, with Opus checking what it finds, to say what would
	// cut tokens or time. Off until you turn it on.
	Advisor bool `json:"advisor,omitzero"`
}

// SignIn is an account of an agent other than Claude Code: who it is, and
// what agtop calls it.
type SignIn struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"` // the agent's own id for the account
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
	Plan  string `json:"plan,omitempty"`
}

// What SwitchOnLimit can be.
const (
	OnLimitAccount = ""      // another account of the same agent
	OnLimitAgent   = "agent" // then the next agent
	OnLimitOff     = "off"
)

// migrate brings a config written by an older agtop up to date. It only
// adds and renames: nothing an older agtop reads is taken away.
func (c *Config) migrate() {
	if c.StayOnAccount && c.SwitchOnLimit == "" {
		c.SwitchOnLimit = OnLimitOff
	}
	if c.GroupBy == "account" {
		// Folders are gone; sessions group by the agent they run.
		c.GroupBy = "agent"
	}
	c.migrateProfiles()
}

// SetSwitchOnLimit sets what agtop does when an account is nearly out.
func (c *Config) SetSwitchOnLimit(v string) {
	c.SwitchOnLimit, c.StayOnAccount = v, v == OnLimitOff
}

// DefaultAgent is the agent new sessions run: its kind.
func (c Config) DefaultAgent() string {
	if c.Dispatch.Kind == "" {
		return "claude"
	}
	return c.Dispatch.Kind
}

// NoteSignIn records an account of another agent, named name (or after
// its email) when it's new, and returns what it's called.
func (c *Config) NoteSignIn(s SignIn, name string) SignIn {
	for i, old := range c.SignIns {
		if old.Kind == s.Kind && old.ID == s.ID {
			s.Name = old.Name
			if s.Email == "" {
				s.Email = old.Email
			}
			if s.Plan == "" {
				s.Plan = old.Plan
			}
			c.SignIns[i] = s
			return s
		}
	}
	if name == "" {
		name, _, _ = strings.Cut(s.Email, "@")
	}
	if name == "" {
		name = s.ID
	}
	s.Name = name
	for n := 2; c.signInNamed(s.Kind, s.Name); n++ {
		s.Name = fmt.Sprintf("%s-%d", name, n)
	}
	c.SignIns = append(c.SignIns, s)
	return s
}

func (c Config) signInNamed(kind, name string) bool {
	for _, s := range c.SignIns {
		if s.Kind == kind && strings.EqualFold(s.Name, name) {
			return true
		}
	}
	return false
}

// SignInsOf are the accounts agtop keeps for agent kind.
func (c Config) SignInsOf(kind string) []SignIn {
	var out []SignIn
	for _, s := range c.SignIns {
		if s.Kind == kind {
			out = append(out, s)
		}
	}
	return out
}

// ForgetSignIn drops an account of another agent.
func (c *Config) ForgetSignIn(kind, id string) {
	var keep []SignIn
	for _, s := range c.SignIns {
		if s.Kind != kind || s.ID != id {
			keep = append(keep, s)
		}
	}
	c.SignIns = keep
	if c.Using[kind] == id {
		delete(c.Using, kind)
	}
}

// Onboarding is what agtop has taught you so far.
type Onboarding struct {
	Steps  []string `json:"steps,omitempty"`
	Tips   []string `json:"tips,omitempty"`
	Hidden bool     `json:"hidden,omitzero"`
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

// Dispatch is how new sessions start: which coding agent, model, effort and
// permission mode. Empty means Claude Code's own default.
type Dispatch struct {
	// Kind is the agent new sessions run: empty is Claude Code.
	Kind       string `json:"kind,omitempty"`
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
	Lean bool `json:"lean,omitzero"`
	// RestMinutes is how long an idle agtop-mode session keeps Claude Code
	// running before stopping it (a message starts it again); 0 is the
	// default, as soon as it's done.
	RestMinutes int `json:"restMinutes,omitzero"`
}

// DefaultRest is how long an idle agtop-mode session keeps Claude Code
// running when RestMinutes isn't set: a moment after it's done, with
// nothing left in the background. An idle Claude Code holds 150-200 MB;
// starting it again takes about a second, and the prompt cache (an hour)
// isn't lost.
const DefaultRest = 3 * time.Second

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

// OldFolders are the folders besides ~/.claude an older agtop was given,
// whose sign-ins and past sessions are still to be taken in.
func (c Config) OldFolders() []claude.Account {
	var out []claude.Account
	for _, a := range c.Folders {
		if a.ConfigDir != "" && a.ConfigDir != claude.DefaultAccount().ConfigDir {
			out = append(out, a)
		}
	}
	return out
}

// RootFolder is ~/.claude's entry in Folders, if it has one: what's kept
// when the older folders are dropped.
func (c Config) RootFolder() []claude.Account {
	for _, a := range c.Folders {
		if a.ConfigDir == claude.DefaultAccount().ConfigDir {
			return []claude.Account{a}
		}
	}
	return nil
}

// ActiveAccount is where every session runs: ~/.claude, signed in as
// whichever login is in use.
func (c Config) ActiveAccount() claude.Account {
	root := claude.DefaultAccount()
	for _, a := range c.Folders {
		if a.ConfigDir == root.ConfigDir && a.Name != "" {
			root.Name = a.Name
		}
	}
	return root
}

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
	copied  copied
}

// copied is the config as Copy last made it, and as JSON: while the
// config is unchanged, Copy hands out that config again rather than
// decoding a fresh one on every load.
type copied struct {
	buf  bytes.Buffer
	json []byte
	cfg  Config
}

func Load() *Store {
	s := &Store{}
	loadJSON(filepath.Join(Dir(), "config.json"), &s.Config)
	loadJSON(filepath.Join(Dir(), "state.json"), &s.Overlay)
	s.Config.migrate()
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

// Copy is the config and overlay as they are now, sharing nothing with s:
// for reading, and only reading, off the UI's goroutine while the UI goes
// on changing s. Copies made while the config is unchanged share theirs.
func (s *Store) Copy() *Store {
	c := &Store{Overlay: Overlay{
		Done: maps.Clone(s.Overlay.Done), Names: maps.Clone(s.Overlay.Names),
		Groups: maps.Clone(s.Overlay.Groups), Moved: maps.Clone(s.Overlay.Moved),
		Seen: maps.Clone(s.Overlay.Seen),
	}}
	// The config is read only, off the UI's goroutine, so copies can share
	// one while it's the same.
	cp := &s.copied
	cp.buf.Reset()
	if jsonx.MarshalWrite(&cp.buf, s.Config) != nil {
		return c
	}
	if cp.json == nil || !bytes.Equal(cp.buf.Bytes(), cp.json) {
		cp.cfg = Config{}
		if jsonx.Unmarshal(cp.buf.Bytes(), &cp.cfg) != nil {
			cp.json = nil
			return c
		}
		cp.json = bytes.Clone(cp.buf.Bytes())
	}
	c.Config = cp.cfg
	return c
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

// KeepBefore copies config.json aside as config.json.<name>, once, before
// a change an older agtop wouldn't make: the copy is never overwritten.
func KeepBefore(name string) {
	path := filepath.Join(Dir(), "config.json")
	aside := path + "." + name
	if _, err := os.Stat(aside); err == nil {
		return
	}
	if b, err := os.ReadFile(path); err == nil {
		_ = os.WriteFile(aside, b, 0o600)
	}
}

func readJSON(path string, v any) {
	if b, err := os.ReadFile(path); err == nil {
		_ = jsonx.Unmarshal(b, v)
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
	if jsonx.Valid(b) {
		_ = jsonx.Unmarshal(b, v)
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
	b, err := jsonx.MarshalIndent(v)
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
	// It's written as it's made: it runs to megabytes, saved every 30s.
	path := filepath.Join(cacheDir(), "costs.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(f, 64<<10)
	err = jsonx.MarshalWrite(w, c)
	if err == nil {
		err = w.Flush()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(path + ".tmp")
		return err
	}
	return os.Rename(path+".tmp", path)
}
