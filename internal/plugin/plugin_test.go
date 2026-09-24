package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// install writes a plugin into AGTOP_HOME, which the test has set, and
// returns its folder.
func install(t *testing.T, m Manifest, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(Root(), m.Name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestValidate(t *testing.T) {
	ok := Manifest{Name: "p", Command: []string{"bin"}}
	cases := []struct {
		name string
		edit func(*Manifest)
		want string // in the error; "" for valid
	}{
		{"minimal", func(*Manifest) {}, ""},
		{"bad name", func(m *Manifest) { m.Name = "P!" }, "name"},
		{"no command", func(m *Manifest) { m.Command = nil }, "command is empty"},
		{"command escapes", func(m *Manifest) { m.Command = []string{"../../bin/sh"} }, "outside"},
		{"absolute interpreter", func(m *Manifest) { m.Command = []string{"/usr/bin/python3", "main.py"} }, ""},
		{"unknown protocol", func(m *Manifest) { m.Protocol = "grpc" }, "protocol"},
		{"mcp with sessions", func(m *Manifest) { m.Protocol = ProtoMCP; m.Sessions = []string{CapList} }, "MCP plugin"},
		{"unknown capability", func(m *Manifest) { m.Sessions = []string{"root"} }, "capability"},
		{"start without workspace", func(m *Manifest) { m.Sessions = []string{CapStart} }, "workspace"},
		{"workspace /", func(m *Manifest) { m.Sessions = []string{CapStart}; m.Workspaces = []string{"/"} }, "cannot be /"},
		{"relative workspace", func(m *Manifest) { m.Workspaces = []string{"src"} }, "absolute"},
		{"home workspace", func(m *Manifest) { m.Sessions = []string{CapStart}; m.Workspaces = []string{"~/src"} }, ""},
		{"network wildcard", func(m *Manifest) { m.Network = []string{"*.example.com:443"} }, "wildcards"},
		{"network no port", func(m *Manifest) { m.Network = []string{"example.com"} }, "host:port"},
		{"network ok", func(m *Manifest) { m.Network = []string{"api.example.com:443"} }, ""},
		{"agent without prompt", func(m *Manifest) {
			m.Agents = map[string]json.RawMessage{"x": json.RawMessage(`{"description":"d"}`)}
		}, "prompt"},
		{"agent bad name", func(m *Manifest) {
			m.Agents = map[string]json.RawMessage{"X Y": json.RawMessage(`{"description":"d","prompt":"p"}`)}
		}, "agent name"},
		{"huge prompt", func(m *Manifest) { m.Prompt = strings.Repeat("x", maxPrompt+1) }, "prompt"},
		{"queue without workspace", func(m *Manifest) { m.Sessions = []string{CapQueue} }, "workspace"},
		{"relative write", func(m *Manifest) { m.Write = []string{"data"} }, "absolute"},
		{"write home", func(m *Manifest) { m.Write = []string{"~"} }, "too broad"},
		{"write ok", func(m *Manifest) { m.Write = []string{"~/.kanban-code"} }, ""},
		{"exec relative", func(m *Manifest) { m.Exec = map[string][]string{"kanban": {"kanban"}} }, "absolute"},
		{"exec bad name", func(m *Manifest) { m.Exec = map[string][]string{"K B": {"/bin/echo"}} }, "exec name"},
		{"exec from mcp", func(m *Manifest) { m.Protocol = ProtoMCP; m.Exec = map[string][]string{"e": {"/bin/echo"}} }, "MCP plugin"},
		{"exec ok", func(m *Manifest) { m.Exec = map[string][]string{"kanban": {"~/.local/bin/kanban", "--json"}} }, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := ok
			c.edit(&m)
			err := m.Validate("/x/" + m.Name)
			switch {
			case c.want == "" && err != nil:
				t.Fatalf("want valid, got %v", err)
			case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
				t.Fatalf("want error with %q, got %v", c.want, err)
			}
		})
	}
	if err := ok.Validate("/x/other"); err == nil {
		t.Fatal("a name that doesn't match its folder was accepted")
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	dir := install(t, Manifest{Name: "p", Command: []string{"bin"}}, nil)
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(`{"name":"p","command":["bin"],"sandbox":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "sandbox") {
		t.Fatalf("an unknown field was accepted: %v", err)
	}
}

func TestApprovalPinsTheFiles(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	dir := install(t, Manifest{Name: "p", Command: []string{"bin"}}, map[string]string{"bin": "#!v1"})
	if _, err := Verify("p"); err == nil {
		t.Fatal("an unapproved plugin verified")
	}
	p, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := Approve(p); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify("p"); err != nil {
		t.Fatalf("approved plugin: %v", err)
	}
	// Changing the program, adding a file, or editing the manifest each
	// undo the approval.
	for _, change := range []func(){
		func() { _ = os.WriteFile(filepath.Join(dir, "bin"), []byte("#!v2"), 0o700) },
		func() { _ = os.WriteFile(filepath.Join(dir, "extra.so"), nil, 0o600) },
		func() { _ = os.Chmod(filepath.Join(dir, "bin"), 0o755) },
	} {
		change()
		if _, err := Verify("p"); err == nil || !strings.Contains(err.Error(), "changed") {
			t.Fatalf("a changed plugin verified: %v", err)
		}
		p, _ = Load(dir)
		_ = Approve(p)
	}
	// The manifest that counts is the one approved, not the one on disk.
	_ = os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(`{"name":"p","command":["bin"],"prompt":"obey me"}`), 0o600)
	if c := ForSession(); slices.Contains(c.Flags, "--append-system-prompt") {
		t.Fatal("an unapproved manifest edit reached sessions")
	}
	if err := Revoke("p"); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify("p"); err == nil {
		t.Fatal("a revoked plugin verified")
	}
}

func TestForSession(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	for _, m := range []Manifest{
		{Name: "a", Command: []string{"a"}, Tools: true, Prompt: "Be kind.",
			Agents: map[string]json.RawMessage{"rev": json.RawMessage(`{"description":"d","prompt":"p"}`)}},
		{Name: "b", Command: []string{"b"}, Protocol: ProtoMCP},
		{Name: "c", Command: []string{"c"}},
	} {
		p, err := Load(install(t, m, nil))
		if err != nil {
			t.Fatal(err)
		}
		if err := Approve(p); err != nil {
			t.Fatal(err)
		}
	}
	c := ForSession()
	if !slices.Equal(c.Servers, []string{"agtop-a", "agtop-b"}) {
		t.Fatalf("servers = %v", c.Servers)
	}
	flags := strings.Join(c.Flags, "\x00")
	if !strings.Contains(flags, "--agents\x00{\"a:rev\":") {
		t.Fatalf("agents flag missing or unnamespaced: %q", c.Flags)
	}
	if !strings.Contains(flags, "--append-system-prompt\x00# From the agtop plugin a\n\nBe kind.") {
		t.Fatalf("prompt flag = %q", c.Flags)
	}
}

func TestEnvIsCleanAndFixed(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "secret")
	t.Setenv("CLAUDE_CONFIG_DIR", "/Users/me/.claude")
	l := Launch{Plugin: Plugin{Manifest: Manifest{Name: "p", Command: []string{"x"},
		Env: map[string]string{"HOME": "/Users/me", "STORE": "${DATA}/db"}}, Dir: "/plug"}, ProxyPort: 4242}
	env := l.Env()
	get := func(k string) string {
		for _, kv := range env {
			if v, ok := strings.CutPrefix(kv, k+"="); ok {
				return v
			}
		}
		return ""
	}
	if get("ANTHROPIC_API_KEY") != "" || get("CLAUDE_CONFIG_DIR") != "" {
		t.Fatal("a secret from agtop's environment reached the plugin")
	}
	if get("HOME") != DataDir("p") {
		t.Fatalf("HOME = %q: the manifest moved it", get("HOME"))
	}
	if get("STORE") != DataDir("p")+"/db" {
		t.Fatalf("STORE = %q", get("STORE"))
	}
	if get("HTTPS_PROXY") != "http://127.0.0.1:4242" || get("AGTOP_IPC_FD") != "3" {
		t.Fatalf("proxy/ipc env missing: %v", env)
	}
}
