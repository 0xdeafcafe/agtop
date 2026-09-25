// Package plugin runs other people's code next to your agents without
// trusting it.
//
// A plugin is a folder in Root() with a plugin.json manifest and a program.
// It runs in its own process, started by the broker (`agtop plugind`), in a
// macOS sandbox that denies everything it wasn't given: it reads its own
// folder and the system's libraries, writes only its data folder, starts no
// other programs, and reaches the network only through agtop's proxy, to the
// hosts its manifest names. It runs at utility QoS, and the broker ends it if
// it grows past its memory limit.
//
// It talks to agtop over one end of a socket pair, handed to it as fd 3: no
// path on disk, so nothing else can connect to either side. The messages are
// JSON-RPC 2.0 in length-prefixed frames. A plugin whose manifest says
// "protocol": "mcp" is an ordinary MCP server spoken to on stdio instead, so
// an off-the-shelf one, a memory server say, runs sandboxed as it is.
//
// Nothing runs until you approve it (`agtop plugin approve`), which records a
// digest of every file in its folder along with the manifest you read. If a
// file changes, the plugin stops running until you approve it again. Agents,
// prompt text and tools all come from the manifest as approved, not as it is
// on disk now.
package plugin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/0xdeafcafe/agtop/internal/state"
)

// Root holds one folder per installed plugin.
func Root() string { return filepath.Join(state.Dir(), "plugins") }

// DataRoot holds each plugin's data folder, the one place it can write. It
// is apart from the plugin's own folder so the digest of that stays fixed.
func DataRoot() string { return filepath.Join(state.Dir(), "plugin-data") }

// DataDir is a plugin's data folder.
func DataDir(name string) string { return filepath.Join(DataRoot(), name) }

// LogPath is where a plugin's stdout and stderr go.
func LogPath(name string) string { return filepath.Join(DataRoot(), name+".log") }

func approvedPath() string { return filepath.Join(Root(), "approved.json") }

// Protocols a plugin can speak.
const (
	ProtoAgtop = "agtop" // framed JSON-RPC on fd 3; the default
	ProtoMCP   = "mcp"   // an MCP server on stdio
)

// Capabilities a plugin can be given over sessions.
const (
	// CapList lists every agtop-mode session, and follows the list as it
	// changes: each one's name, folder, repo and branch, state, what it's
	// doing, its cost and context. Not what was said.
	CapList = "list"
	// CapStart starts sessions in the manifest's workspaces.
	CapStart = "start"
	// CapRead follows what the plugin's own sessions say.
	CapRead = "read"
	// CapSend sends messages to the plugin's own sessions.
	CapSend = "send"
	// CapControl interrupts and stops the plugin's own sessions.
	CapControl = "control"
	// CapQueue queues messages to any session in the manifest's
	// workspaces, as the plugin, never one that skips asking you. It goes
	// when the session's turn ends, as a queued message of yours does.
	CapQueue = "queue"
)

var caps = []string{CapList, CapStart, CapRead, CapSend, CapControl, CapQueue}

// Manifest is plugin.json.
type Manifest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version,omitempty"`
	// Command is the program and its arguments. The program is a path in
	// the plugin's folder, or an absolute one (an interpreter, say).
	Command  []string          `json:"command"`
	Protocol string            `json:"protocol,omitempty"`
	Env      map[string]string `json:"env,omitempty"` // ${DATA} and ${PLUGIN} are expanded
	// Tools offers the plugin's tools to every agtop-mode session, as
	// mcp__agtop-<name>__<tool>. They ask before running, like any tool.
	// An MCP plugin always offers them.
	Tools    bool     `json:"tools,omitempty"`
	Sessions []string `json:"sessions,omitempty"` // capabilities, see Cap*
	// Workspaces are the folders sessions it starts may run in.
	Workspaces []string `json:"workspaces,omitempty"`
	// Network is the host:port pairs it may reach, through agtop's proxy.
	Network []string `json:"network,omitempty"`
	// Read is more paths it may read, for an interpreter's libraries or
	// another tool's files.
	Read []string `json:"read,omitempty"`
	// Write is more paths it may write (and read), another tool's files
	// say, beyond its data folder.
	Write []string `json:"write,omitempty"`
	// Exec names programs agtop runs for it, outside the sandbox, as you:
	// each a fixed command line the plugin may add arguments to. It is how
	// a plugin drives another tool's CLI.
	Exec     map[string][]string `json:"exec,omitempty"`
	MemoryMB int                 `json:"memoryMB,omitempty"` // default DefaultMemoryMB
	// Agents are subagents given to every agtop-mode session, as Claude
	// Code's --agents takes them; each is named <plugin>:<agent>.
	Agents map[string]json.RawMessage `json:"agents,omitempty"`
	// Prompt is added to every agtop-mode session's system prompt.
	Prompt string `json:"prompt,omitempty"`
	// Sidebar lets it arrange agtop's agent list with sidebar.set: its own
	// sections, and a name for each agent. See Sidebar.
	Sidebar bool `json:"sidebar,omitempty"`
}

// DefaultMemoryMB is the memory limit a manifest doesn't set.
const DefaultMemoryMB = 256

// Limits on what a manifest can carry into every session.
const (
	maxPrompt = 16 << 10
	maxAgents = 64 << 10
)

var nameRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)
var hostRE = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
var agentRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,40}$`)

// Plugin is an installed plugin.
type Plugin struct {
	Manifest
	Dir string // its folder
}

// Proto is the protocol it speaks.
func (m Manifest) Proto() string {
	if m.Protocol == "" {
		return ProtoAgtop
	}
	return m.Protocol
}

// HasTools says whether it offers tools to sessions.
func (m Manifest) HasTools() bool { return m.Tools || m.Proto() == ProtoMCP }

// Can says whether it was given capability c.
func (m Manifest) Can(c string) bool { return slices.Contains(m.Sessions, c) }

// Memory is its memory limit in bytes.
func (m Manifest) Memory() uint64 {
	if m.MemoryMB <= 0 {
		return DefaultMemoryMB << 20
	}
	return uint64(m.MemoryMB) << 20
}

// Server is the MCP server name its tools are offered under.
func (m Manifest) Server() string { return ServerPrefix + m.Name }

// ServerPrefix starts every plugin's MCP server name.
const ServerPrefix = "agtop-"

// NameOf is the plugin behind an MCP server name, if it is one.
func NameOf(server string) (string, bool) {
	return strings.CutPrefix(server, ServerPrefix)
}

// Validate checks a manifest read from dir.
func (m Manifest) Validate(dir string) error {
	if !nameRE.MatchString(m.Name) {
		return fmt.Errorf("name %q: use lowercase letters, digits and dashes, starting with a letter", m.Name)
	}
	if filepath.Base(dir) != m.Name {
		return fmt.Errorf("name %q does not match its folder %q", m.Name, filepath.Base(dir))
	}
	if len(m.Command) == 0 || m.Command[0] == "" {
		return errors.New("command is empty")
	}
	if !filepath.IsAbs(m.Command[0]) {
		if p := filepath.Clean(m.Command[0]); p == ".." || strings.HasPrefix(p, "../") {
			return fmt.Errorf("command %q is outside the plugin's folder", m.Command[0])
		}
	}
	switch m.Proto() {
	case ProtoAgtop:
	case ProtoMCP:
		if len(m.Sessions) > 0 {
			return errors.New("an MCP plugin cannot be given session capabilities: it has no way to ask for them")
		}
	default:
		return fmt.Errorf("protocol %q: use %q or %q", m.Protocol, ProtoAgtop, ProtoMCP)
	}
	for _, c := range m.Sessions {
		if !slices.Contains(caps, c) {
			return fmt.Errorf("session capability %q: use one of %s", c, strings.Join(caps, ", "))
		}
	}
	if m.Can(CapStart) && len(m.Workspaces) == 0 {
		return errors.New(`"start" needs at least one workspace to start sessions in`)
	}
	for _, w := range m.Workspaces {
		if !filepath.IsAbs(expandHome(w)) {
			return fmt.Errorf("workspace %q is not an absolute path", w)
		}
		if filepath.Clean(expandHome(w)) == "/" {
			return errors.New("a workspace cannot be /")
		}
	}
	if m.Can(CapQueue) && len(m.Workspaces) == 0 {
		return errors.New(`"queue" needs at least one workspace whose sessions it may queue to`)
	}
	for _, r := range m.Read {
		if !filepath.IsAbs(expandHome(r)) {
			return fmt.Errorf("read path %q is not absolute", r)
		}
	}
	for _, w := range m.Write {
		if !filepath.IsAbs(expandHome(w)) {
			return fmt.Errorf("write path %q is not absolute", w)
		}
		if c := filepath.Clean(expandHome(w)); c == "/" || c == filepath.Clean(expandHome("~")) {
			return fmt.Errorf("write path %q is too broad", w)
		}
	}
	if len(m.Exec) > 0 && m.Proto() != ProtoAgtop {
		return errors.New("an MCP plugin cannot be given programs to run: it has no way to ask for them")
	}
	for name, argv := range m.Exec {
		if !agentRE.MatchString(name) {
			return fmt.Errorf("exec name %q: use lowercase letters, digits and dashes", name)
		}
		if len(argv) == 0 || !filepath.IsAbs(expandHome(argv[0])) {
			return fmt.Errorf("exec %q: the program must be an absolute path", name)
		}
	}
	for _, n := range m.Network {
		if _, _, err := SplitHostPort(n); err != nil {
			return err
		}
	}
	for k := range m.Env {
		if k == "" || strings.ContainsAny(k, "=\x00") {
			return fmt.Errorf("env name %q is not valid", k)
		}
	}
	if m.MemoryMB < 0 || m.MemoryMB > 8192 {
		return fmt.Errorf("memoryMB %d: use 1 to 8192", m.MemoryMB)
	}
	if m.Sidebar && m.Proto() != ProtoAgtop {
		return errors.New("an MCP plugin cannot be given the sidebar: it has no way to set it")
	}
	if len(m.Prompt) > maxPrompt {
		return fmt.Errorf("prompt is over %d bytes", maxPrompt)
	}
	size := 0
	for name, a := range m.Agents {
		if !agentRE.MatchString(name) {
			return fmt.Errorf("agent name %q: use lowercase letters, digits and dashes", name)
		}
		var def struct {
			Description string `json:"description"`
			Prompt      string `json:"prompt"`
		}
		if err := json.Unmarshal(a, &def); err != nil || def.Description == "" || def.Prompt == "" {
			return fmt.Errorf("agent %q needs a description and a prompt", name)
		}
		size += len(a)
	}
	if size > maxAgents {
		return fmt.Errorf("agents are over %d bytes", maxAgents)
	}
	return nil
}

// SplitHostPort reads a network entry, host:port.
func SplitHostPort(s string) (string, int, error) {
	h, p, err := net.SplitHostPort(s)
	if err != nil {
		return "", 0, fmt.Errorf("network %q: use host:port", s)
	}
	port, err := strconv.Atoi(p)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("network %q: bad port", s)
	}
	if !hostRE.MatchString(h) {
		return "", 0, fmt.Errorf("network %q: bad host (wildcards aren't allowed)", s)
	}
	return strings.ToLower(h), port, nil
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[1:])
	}
	return p
}

// ExecArgv is the command line of the program the manifest names, with ~
// expanded, or nil.
func (m Manifest) ExecArgv(name string) []string {
	argv, ok := m.Exec[name]
	if !ok {
		return nil
	}
	return append([]string{expandHome(argv[0])}, argv[1:]...)
}

// WorkspaceDirs are the manifest's workspaces with ~ expanded and symlinks
// resolved; one that doesn't exist is left out.
func (m Manifest) WorkspaceDirs() []string {
	var out []string
	for _, w := range m.Workspaces {
		if r, err := filepath.EvalSymlinks(expandHome(w)); err == nil {
			out = append(out, r)
		}
	}
	return out
}

// Load reads the plugin in dir.
func Load(dir string) (Plugin, error) {
	b, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return Plugin{}, err
	}
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Plugin{}, fmt.Errorf("%s: %w", filepath.Join(dir, "plugin.json"), err)
	}
	if err := m.Validate(dir); err != nil {
		return Plugin{}, fmt.Errorf("%s: %w", filepath.Base(dir), err)
	}
	return Plugin{Manifest: m, Dir: dir}, nil
}

// Installed lists the plugins in Root(), and the folders that aren't valid
// plugins with why.
func Installed() ([]Plugin, map[string]error) {
	ents, _ := os.ReadDir(Root())
	var out []Plugin
	bad := map[string]error{}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		p, err := Load(filepath.Join(Root(), e.Name()))
		if err != nil {
			bad[e.Name()] = err
			continue
		}
		out = append(out, p)
	}
	return out, bad
}

// Digest is a hash of every file in the plugin's folder: names, modes,
// contents and where symlinks point.
func Digest(dir string) (string, error) {
	// A plugin being worked on may be a link to where it's written.
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		dir = r
	}
	h := sha256.New()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		info, err := d.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%q %o\n", rel, info.Mode())
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			t, err := os.Readlink(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(h, "-> %q\n", t)
		case info.Mode().IsRegular():
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(h, "%d\n", info.Size())
			_, err = io.Copy(h, f)
			f.Close()
			return err
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Approval is what you agreed to run.
type Approval struct {
	Digest     string    `json:"digest"`
	Manifest   Manifest  `json:"manifest"`
	ApprovedAt time.Time `json:"approvedAt"`
}

// Approvals reads every approval, by plugin name.
func Approvals() map[string]Approval {
	out := map[string]Approval{}
	b, err := os.ReadFile(approvedPath())
	if err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

func saveApprovals(a map[string]Approval) error {
	if err := os.MkdirAll(Root(), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	tmp := approvedPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, approvedPath())
}

// Approve records that p, as it is now, may run.
func Approve(p Plugin) error {
	d, err := Digest(p.Dir)
	if err != nil {
		return err
	}
	a := Approvals()
	a[p.Name] = Approval{Digest: d, Manifest: p.Manifest, ApprovedAt: time.Now()}
	return saveApprovals(a)
}

// Revoke withdraws a plugin's approval.
func Revoke(name string) error {
	a := Approvals()
	if _, ok := a[name]; !ok {
		return fmt.Errorf("%s is not approved", name)
	}
	delete(a, name)
	if err := saveApprovals(a); err != nil {
		return err
	}
	return RemoveSidebar(name)
}

// Verify says whether the plugin on disk is the one approved, returning the
// approved manifest to run it by.
func Verify(name string) (Plugin, error) {
	a, ok := Approvals()[name]
	if !ok {
		return Plugin{}, fmt.Errorf("%s is not approved", name)
	}
	dir := filepath.Join(Root(), name)
	d, err := Digest(dir)
	if err != nil {
		return Plugin{}, err
	}
	if d != a.Digest {
		return Plugin{}, fmt.Errorf("%s changed since it was approved; run agtop plugin approve %s", name, name)
	}
	return Plugin{Manifest: a.Manifest, Dir: dir}, nil
}

// Contributions is what approved plugins add to a session's Claude Code.
type Contributions struct {
	// Flags go on Claude Code's command line: --agents and
	// --append-system-prompt.
	Flags []string
	// Servers are the plugins' MCP servers, to register at initialize.
	Servers []string
}

// ForSession gathers the contributions of every approved plugin. It reads
// only the approvals, so it costs one small file read, and what goes in is
// what was approved.
func ForSession() Contributions {
	var c Contributions
	a := Approvals()
	names := make([]string, 0, len(a))
	for n := range a {
		names = append(names, n)
	}
	sort.Strings(names)
	agents := map[string]json.RawMessage{}
	var prompts []string
	for _, n := range names {
		m := a[n].Manifest
		if m.HasTools() {
			c.Servers = append(c.Servers, m.Server())
		}
		for an, def := range m.Agents {
			agents[m.Name+":"+an] = def
		}
		if p := strings.TrimSpace(m.Prompt); p != "" {
			prompts = append(prompts, "# From the agtop plugin "+m.Name+"\n\n"+p)
		}
	}
	if len(agents) > 0 {
		b, _ := json.Marshal(agents)
		c.Flags = append(c.Flags, "--agents", string(b))
	}
	if len(prompts) > 0 {
		c.Flags = append(c.Flags, "--append-system-prompt", strings.Join(prompts, "\n\n"))
	}
	return c
}
