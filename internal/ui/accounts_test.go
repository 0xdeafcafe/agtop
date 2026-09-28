package ui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/agent/usage"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// fakeAgent is an agent for Accounts to list: installed when its program
// is in the test's PATH, switchable when switched is set.
type fakeAgent struct {
	kind     agent.Kind
	title    string
	dir      string
	switched *[]string
}

func (f fakeAgent) Kind() agent.Kind                                            { return f.kind }
func (f fakeAgent) Name() string                                                { return f.title }
func (fakeAgent) Features() map[agent.Feature]agent.Support                     { return nil }
func (fakeAgent) Level() agent.Level                                            { return agent.LevelPreview }
func (f fakeAgent) Profiles() []agent.Profile                                   { return []agent.Profile{{Kind: f.kind, Dir: f.dir}} }
func (f fakeAgent) Program() (string, []string)                                 { return "agtop-fake-" + string(f.kind), nil }
func (fakeAgent) Start(context.Context, agent.StartOptions) (agent.Conn, error) { return nil, nil }

type switchAgent struct{ fakeAgent }

func (s switchAgent) Current(agent.Profile) (agent.Account, error) {
	return agent.Account{Kind: s.kind, ID: "a1", Email: "one@example.com"}, nil
}
func (s switchAgent) Switch(_ agent.Profile, a agent.Account) error {
	*s.switched = append(*s.switched, a.ID)
	return nil
}
func (switchAgent) SignIn(agent.Profile) (*exec.Cmd, func() (agent.Account, error), error) {
	return nil, nil, nil
}
func (switchAgent) Forget(agent.Account) error { return nil }

// accountsModel is a Model with Claude Code (two logins), a switchable
// agent with two accounts, one that signs in by itself, and one that
// isn't installed.
func accountsModel(t *testing.T) (*Model, *[]string) {
	t.Helper()
	t.Setenv("AGTOP_HOME", t.TempDir())
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	var switched []string
	fakes := []agent.Adapter{
		fakeAgent{kind: "claude", title: "Claude Code"},
		switchAgent{fakeAgent{kind: "zcodex", title: "ZCodex", dir: "/z/.zcodex", switched: &switched}},
		fakeAgent{kind: "zplain", title: "ZPlain", dir: "/z/.zplain"},
		fakeAgent{kind: "zgone", title: "ZGone", dir: "/z/.zgone"},
	}
	for _, f := range fakes {
		// The real agent of that kind comes back after: the built-in one
		// is what every other test's sessions run.
		if was, ok := agent.Get(f.Kind()); ok {
			t.Cleanup(func() { agent.Register(was) })
		}
		agent.Register(f)
		if f.Kind() != "zgone" {
			name, _ := f.(agent.Programmer).Program()
			if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	agent.Recheck()
	t.Cleanup(agent.Recheck)

	m := &Model{store: &state.Store{}, previews: map[string]previewEntry{}, w: 160, h: 50, lastState: map[string]string{}}
	now := time.Now()
	win := func(p float64) usage.Quota {
		return usage.Quota{FetchedAt: now, Windows: []usage.Window{{ID: "five_hour", Label: "5h", Percent: p}}}
	}
	m.snap = &fleet.Snapshot{At: now, Logins: []fleet.LoginView{
		{Current: true, Quota: win(40)}, {Quota: win(10)},
	}}
	m.snap.Logins[0].Name, m.snap.Logins[0].Email = "work", "me@work.example"
	m.snap.Logins[1].Name, m.snap.Logins[1].Email = "home", "me@home.example"
	m.store.Config.SignIns = []state.SignIn{
		{Kind: "zcodex", ID: "a1", Name: "one", Email: "one@example.com"},
		{Kind: "zcodex", ID: "a2", Name: "two", Email: "two@example.com"},
	}
	m.quotas = map[string]usage.Quota{"zcodex:a1": win(97), "zcodex:a2": win(20)}
	m.accts.ready()
	m.accts.now["zcodex"] = "a1"
	m.rebuild()
	m.openDialog(tabAccounts)
	return m, &switched
}

func rowNames(rows []acctRow) []string {
	var out []string
	for _, r := range rows {
		if !strings.HasPrefix(string(r.kind), "z") && r.kind != "claude" {
			continue // another test's agent
		}
		n := r.name()
		if r.head {
			n = "#" + n
		}
		out = append(out, n)
	}
	return out
}

// Accounts groups accounts under their agent, lists only installed
// agents, and follows the order you set.
func TestAccountsGroupedByAgent(t *testing.T) {
	m, _ := accountsModel(t)
	got := strings.Join(rowNames(m.accountRows()), ",")
	if got != "#Claude Code,work,home,#ZCodex,one,two,#ZPlain" {
		t.Fatalf("rows: %s", got)
	}
	// J moves the agent under the cursor later.
	m.store.Config.AgentOrder = []string{"claude", "zcodex", "zplain"}
	m.dialog.cursor = 0
	m.accountsKey("J")
	if got := strings.Join(rowNames(m.accountRows()), ","); got != "#ZCodex,one,two,#Claude Code,work,home,#ZPlain" {
		t.Fatalf("after J: %s", got)
	}
	if m.dialog.cursor != 3 {
		t.Fatalf("the cursor stayed at %d, not with the agent it moved", m.dialog.cursor)
	}
	// 3 jumps to the third agent.
	m.accountsKey("3")
	if r := m.accountRows()[m.dialog.cursor]; !r.head || r.kind != "zplain" {
		t.Fatalf("3 went to %+v", r)
	}
	// p makes it the default: new sessions run it.
	m.accountsKey("p")
	if m.store.Config.DefaultAgent() != "zplain" || m.startKind() != "zplain" {
		t.Fatalf("default is %q", m.store.Config.DefaultAgent())
	}
	body := m.accountsBody(150)
	for i, l := range body {
		if w := ansi.StringWidth(l); w > 150 {
			t.Fatalf("line %d is %d wide: %s", i, w, ansi.Strip(l))
		}
	}
	plain := ansi.Strip(strings.Join(body, "\n"))
	for _, want := range []string{"New sessions run ZPlain", "★ ZPlain", "one@example.com", "ZGone"} {
		if strings.Contains(plain, want) != (want != "ZGone") {
			t.Errorf("want %q shown %v in:\n%s", want, want != "ZGone", plain)
		}
	}
}

// enter on another agent's account switches its home to it.
func TestAccountsSwitchAnotherAgent(t *testing.T) {
	m, switched := accountsModel(t)
	rows := m.accountRows()
	for i, r := range rows {
		if r.name() == "two" {
			m.dialog.cursor = i
		}
	}
	cmd := m.accountsKey("enter")
	if cmd == nil {
		t.Fatal("enter did nothing")
	}
	msg := cmd().(acctMsg)
	msg.applyTo(m)
	if len(*switched) != 1 || (*switched)[0] != "a2" || m.accts.now["zcodex"] != "a2" {
		t.Fatalf("switched %v, now on %q", *switched, m.accts.now["zcodex"])
	}
}

// An account nearly out switches to the one with the most room, unless
// you asked agtop to stay put.
func TestAccountsSwitchOnLimit(t *testing.T) {
	m, switched := accountsModel(t)
	m.store.Config.SetSwitchOnLimit(state.OnLimitOff)
	if cmd := m.checkLimits(); cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("switched while told to stay: %v", msg)
		}
	}
	m.store.Config.SetSwitchOnLimit(state.OnLimitAccount)
	cmd := m.checkLimits()
	if cmd == nil {
		t.Fatal("no switch at 97%")
	}
	for _, msg := range flatten(cmd) {
		if am, ok := msg.(acctMsg); ok {
			am.applyTo(m)
		}
	}
	if len(*switched) != 1 || (*switched)[0] != "a2" {
		t.Fatalf("switched %v", *switched)
	}
}

// Across agents: once every account of the default agent is nearly out,
// new sessions run the next agent in your order, and go back after.
func TestAccountsSpillToNextAgent(t *testing.T) {
	m, _ := accountsModel(t)
	m.store.Config.SetSwitchOnLimit(state.OnLimitAgent)
	m.store.Config.AgentOrder = []string{"claude", "zcodex", "zplain"}
	for i := range m.snap.Logins {
		m.snap.Logins[i].Quota.Windows[0].Percent = 99
	}
	m.accts.switchedAt["zcodex"] = time.Now() // leave it be
	m.checkLimits()
	if m.startKind() != "zcodex" {
		t.Fatalf("new sessions run %q", m.startKind())
	}
	m.snap.Logins[1].Quota.Windows[0].Percent = 30
	m.checkLimits()
	if m.startKind() != "claude" {
		t.Fatalf("new sessions still run %q", m.startKind())
	}
}

// flatten runs a command and any batch it makes, for their messages.
func flatten(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, flatten(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

type lesserFake struct{ fakeAgent }

func (lesserFake) Lesser() (string, []string, string) {
	return "agtop-fake-gh", nil, "install zlesser's CLI"
}

// An agent there only through a lesser program shows in Accounts, but
// picking it for new sessions says what to install rather than failing.
func TestAccountsLesserAgent(t *testing.T) {
	m, _ := accountsModel(t)
	agent.Register(lesserFake{fakeAgent{kind: "ylesser", title: "ZLesser", dir: "/y/.ylesser"}})
	bin := os.Getenv("PATH")
	if err := os.WriteFile(filepath.Join(bin, "agtop-fake-gh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	agent.Recheck()
	found := false
	for _, r := range m.accountRows() {
		found = found || r.head && r.kind == "ylesser"
	}
	if !found {
		t.Fatal("ZLesser isn't in Accounts")
	}
	m.withAgent("ylesser")
	if m.store.Config.DefaultAgent() == "ylesser" {
		t.Fatal("it became the default though it can't run sessions")
	}
	if !strings.Contains(ansi.Strip(strings.Join(m.accountsBody(150), "\n")), "without its CLI") {
		t.Fatal("no hint in Accounts")
	}
}
