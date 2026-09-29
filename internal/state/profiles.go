package state

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/0xdeafcafe/agtop/internal/agent"
)

// A profile is a named list of providers (the coding agents: Claude Code,
// Codex, Copilot…) plus what to do when they run out. Accounts within a
// provider rotate as they always have; a profile only says which providers
// a session may use, in which order. Each session gets one: the one picked
// for it, else the one its folder's rule names, else the default.

// Profile is a named list of providers and a policy.
type Profile struct {
	Name string `json:"name"`
	// Providers are the agents' kinds, in the order new sessions try them.
	Providers []string `json:"providers,omitempty"`
	// Mix is where new sessions go once every account of the first
	// provider is nearly out: "" or "stay" waits on it, "mix" moves on to
	// the next provider that has room.
	Mix string `json:"mix,omitempty"`
	// OnLimit is what a running conversation does when a usage limit
	// stops it: "wait" for the reset, "" or "account" moves to another
	// account of the same provider, and "handoff" does that, then hands
	// the conversation to the next provider when none has room.
	OnLimit string `json:"onLimit,omitempty"`
}

// What Profile.Mix and Profile.OnLimit can be.
const (
	MixStay = "stay"
	MixMix  = "mix"

	LimitWait    = "wait"
	LimitAccount = "account"
	LimitHandoff = "handoff"
)

// DefaultProfileName is what the profile made from an older config is
// called.
const DefaultProfileName = "Default"

// LoginsKind is the agent whose accounts are Config.Logins, and whose
// Start is kept in Dispatch's own fields, where older agtops read it.
const LoginsKind = string(agent.LegacyKind)

// FolderRule gives every session started in Path, or a folder inside it,
// the profile named Profile. Path may start with ~.
type FolderRule struct {
	Path    string `json:"path"`
	Profile string `json:"profile"`
}

// Mixes is whether new sessions move on to the next provider once the
// first is out.
func (p Profile) Mixes() bool { return p.Mix == MixMix }

// Limit is what a running conversation does at a usage limit.
func (p Profile) Limit() string {
	if p.OnLimit == "" {
		return LimitAccount
	}
	return p.OnLimit
}

// Has is whether the profile lists provider kind.
func (p Profile) Has(kind string) bool { return slices.Contains(p.Providers, kind) }

// Installed are the profile's providers agtop can run sessions of here,
// in order.
func (p Profile) Installed() []string {
	var out []string
	for _, k := range p.Providers {
		if agent.Runs(agent.Kind(k)) {
			out = append(out, k)
		}
	}
	return out
}

// ProfileNamed is the profile called name, ignoring case. A provider
// that no profile is named after is a profile of its own, of it alone:
// "#profile ollama" runs the next session there, beside the rest.
func (c Config) ProfileNamed(name string) (Profile, bool) {
	for _, p := range c.Profiles {
		if strings.EqualFold(p.Name, name) {
			return p, true
		}
	}
	if k := agent.Kind(strings.ToLower(name)); name != "" {
		if _, ok := agent.Get(k); ok {
			return Profile{Name: string(k), Providers: []string{string(k)}}, true
		}
	}
	return Profile{}, false
}

// Default is the profile a session gets when nothing else says.
func (c Config) Default() Profile {
	if p, ok := c.ProfileNamed(c.DefaultProfile); ok {
		return p
	}
	if len(c.Profiles) > 0 {
		return c.Profiles[0]
	}
	return c.legacyProfile()
}

// ProfileFor is the profile a session in cwd gets: explicit when it names
// one, else the longest folder rule cwd is in, else the default.
func (c Config) ProfileFor(cwd, explicit string) Profile {
	if explicit != "" {
		if p, ok := c.ProfileNamed(explicit); ok {
			return p
		}
	}
	if r, ok := c.RuleFor(cwd); ok {
		if p, ok := c.ProfileNamed(r.Profile); ok {
			return p
		}
	}
	return c.Default()
}

// RuleFor is the folder rule for cwd: the one with the longest path cwd
// is in.
func (c Config) RuleFor(cwd string) (FolderRule, bool) {
	if cwd == "" {
		return FolderRule{}, false
	}
	cwd = filepath.Clean(ExpandHome(cwd))
	best, n := FolderRule{}, -1
	for _, r := range c.FolderRules {
		root := filepath.Clean(ExpandHome(r.Path))
		if r.Path == "" || !within(cwd, root) || len(root) <= n {
			continue
		}
		best, n = r, len(root)
	}
	return best, n >= 0
}

// within is whether dir is root or a folder inside it.
func within(dir, root string) bool {
	if dir == root || root == string(filepath.Separator) {
		return true
	}
	return strings.HasPrefix(dir, root+string(filepath.Separator))
}

// ExpandHome turns a leading ~ into the home folder.
func ExpandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~"))
}

// Seat is one account of a provider as the picker sees it.
type Seat struct {
	ID, Name string
	Current  bool    // the one the provider is signed in as
	Out      bool    // a fresh reading says it's nearly out
	Used     float64 // its tightest window, in percent
}

// Room is what agtop knows of each provider's accounts, by kind. A
// provider with none listed is taken to have room: its own sign-in, whose
// limits agtop can't read or hasn't yet.
type Room map[string][]Seat

// Pick is where a session starts: a provider, and the account of it with
// room. Wait is set when every account of every provider the profile
// allows is nearly out: the session starts there anyway and waits.
type Pick struct {
	Kind    string
	Account Seat
	Wait    bool
}

// seat is the account of a provider to start on: the one in use while it
// has room, else the one with the most room. ok is false when all are out.
func (r Room) seat(kind string) (Seat, bool) {
	seats := r[kind]
	if len(seats) == 0 {
		return Seat{}, true
	}
	var best *Seat
	for i, s := range seats {
		if s.Out {
			continue
		}
		if s.Current {
			return s, true
		}
		if best == nil || s.Used < best.Used {
			best = &seats[i]
		}
	}
	if best == nil {
		for _, s := range seats {
			if s.Current {
				return s, false
			}
		}
		return seats[0], false
	}
	return *best, true
}

// Pick is the provider and account a new session starts on, given what
// room each has: the first installed provider while any of its accounts
// has room; then, when the profile mixes, the next that has. ok is false
// when none of its providers is installed.
func (p Profile) Pick(room Room) (Pick, bool) {
	inst := p.Installed()
	if len(inst) == 0 {
		return Pick{}, false
	}
	first, ok := room.seat(inst[0])
	if ok {
		return Pick{Kind: inst[0], Account: first}, true
	}
	if p.Mixes() {
		if next, ok := p.Next(inst[0], room); ok {
			return next, true
		}
	}
	return Pick{Kind: inst[0], Account: first, Wait: true}, true
}

// Next is the first installed provider after kind with room, for a
// conversation handed on from it. Providers before kind aren't tried: they
// were out, or the profile prefers kind to them.
func (p Profile) Next(kind string, room Room) (Pick, bool) {
	inst := p.Installed()
	at := slices.Index(inst, kind)
	for _, k := range inst[at+1:] {
		if s, ok := room.seat(k); ok {
			return Pick{Kind: k, Account: s}, true
		}
	}
	return Pick{}, false
}

// PickFor is the account of provider kind to use under this profile: for
// work that has to run on that provider (Claude Code's advisor, say). ok
// is false when the profile doesn't list it, it isn't installed, or every
// account of it is nearly out.
func (p Profile) PickFor(kind string, room Room) (Pick, bool) {
	if !p.Has(kind) || !agent.Runs(agent.Kind(kind)) {
		return Pick{}, false
	}
	s, ok := room.seat(kind)
	return Pick{Kind: kind, Account: s, Wait: !ok}, ok
}

// legacyProfile is the profile an older config meant: its default agent,
// then its order, with its choice of what to do when nearly out.
func (c Config) legacyProfile() Profile {
	p := Profile{Name: DefaultProfileName, Providers: []string{c.DefaultAgent()}}
	for _, k := range c.AgentOrder {
		if !p.Has(k) {
			p.Providers = append(p.Providers, k)
		}
	}
	switch c.SwitchOnLimit {
	case OnLimitAgent:
		p.Mix = MixMix
		// An older agtop moved on through every installed agent, those not
		// in its order after, by name.
		for _, a := range agent.All() {
			if k := string(a.Kind()); !p.Has(k) {
				p.Providers = append(p.Providers, k)
			}
		}
	case OnLimitOff:
		p.OnLimit = LimitWait
	}
	return p
}

// migrateProfiles makes the Default profile from an older config, once.
func (c *Config) migrateProfiles() {
	if len(c.Profiles) > 0 {
		return
	}
	p := c.legacyProfile()
	c.Profiles, c.DefaultProfile = []Profile{p}, p.Name
}

// SyncLegacy writes the default profile back into the fields older agtops
// read: the default agent, the order, and what happens when nearly out.
// Call it after changing profiles.
func (c *Config) SyncLegacy() {
	p := c.Default()
	if len(p.Providers) > 0 {
		c.Dispatch.Kind = p.Providers[0]
	}
	c.AgentOrder = append([]string(nil), p.Providers...)
	switch {
	case p.Limit() == LimitWait:
		c.SetSwitchOnLimit(OnLimitOff)
	case p.Mixes():
		c.SetSwitchOnLimit(OnLimitAgent)
	default:
		c.SetSwitchOnLimit(OnLimitAccount)
	}
}

// SetProfile adds p, or replaces the profile called old (which may be
// p's own name), keeping rules and the default pointing at it.
func (c *Config) SetProfile(old string, p Profile) {
	i := slices.IndexFunc(c.Profiles, func(q Profile) bool { return strings.EqualFold(q.Name, old) })
	if i < 0 {
		c.Profiles = append(c.Profiles, p)
	} else {
		c.Profiles[i] = p
	}
	if old != "" && old != p.Name {
		if strings.EqualFold(c.DefaultProfile, old) {
			c.DefaultProfile = p.Name
		}
		for j, r := range c.FolderRules {
			if strings.EqualFold(r.Profile, old) {
				c.FolderRules[j].Profile = p.Name
			}
		}
	}
	c.SyncLegacy()
}

// DeleteProfile drops the profile called name, and the folder rules that
// name it. The last profile stays: there's always a default.
func (c *Config) DeleteProfile(name string) bool {
	if len(c.Profiles) < 2 {
		return false
	}
	c.Profiles = slices.DeleteFunc(c.Profiles, func(p Profile) bool { return strings.EqualFold(p.Name, name) })
	c.FolderRules = slices.DeleteFunc(c.FolderRules, func(r FolderRule) bool { return strings.EqualFold(r.Profile, name) })
	if strings.EqualFold(c.DefaultProfile, name) {
		c.DefaultProfile = c.Profiles[0].Name
	}
	c.SyncLegacy()
	return true
}

// SetDefaultProfile makes the profile called name the default.
func (c *Config) SetDefaultProfile(name string) bool {
	p, ok := c.ProfileNamed(name)
	if ok {
		c.DefaultProfile = p.Name
		c.SyncLegacy()
	}
	return ok
}

// SetDefaultProvider puts provider kind first in the default profile, so
// new sessions run it.
func (c *Config) SetDefaultProvider(kind string) {
	p := c.Default()
	p.Providers = append([]string{kind}, slices.DeleteFunc(slices.Clone(p.Providers), func(k string) bool { return k == kind })...)
	c.SetProfile(p.Name, p)
}

// SetRule gives folder path the profile called name; an empty name drops
// the folder's rule.
func (c *Config) SetRule(path, name string) {
	clean := func(p string) string { return filepath.Clean(ExpandHome(p)) }
	c.FolderRules = slices.DeleteFunc(c.FolderRules, func(r FolderRule) bool { return clean(r.Path) == clean(path) })
	if name != "" {
		c.FolderRules = append(c.FolderRules, FolderRule{Path: path, Profile: name})
	}
}
