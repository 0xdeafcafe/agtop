package ui

import (
	"cmp"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/fleet"
	"github.com/0xdeafcafe/rush/internal/plugin"
	"github.com/0xdeafcafe/rush/internal/state"
)

// [ and ] go through Settings' pages, round the ends, as tab and
// shift+tab do.
func TestSettingsPagesBrackets(t *testing.T) {
	m, _ := benchModel(140, 50)
	m.setView(placeSettings)
	m.setSettingsPage(pageProviders)
	n := len(m.settingsPages())
	m.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	if m.dialog.page != pageCapabilities {
		t.Fatalf("] went to page %d, not Capabilities", m.dialog.page)
	}
	m.Update(tea.KeyPressMsg{Code: '[', Text: "["})
	m.Update(tea.KeyPressMsg{Code: '[', Text: "["})
	if m.dialog.page != n-1 {
		t.Fatalf("[ from Providers went to page %d, not the last (%d)", m.dialog.page, n-1)
	}
	m.setSettingsPage(pageGeneral)
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.dialog == nil || m.dialog.page != pageGeneral+1 {
		t.Fatal("tab didn't go to Settings' next page")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.dialog == nil || m.dialog.page != pageGeneral {
		t.Fatal("shift+tab didn't go back a page")
	}
	if !strings.Contains(ansi.Strip(m.pages()), "[ ]") {
		t.Fatalf("the page strip doesn't say [ ]: %q", ansi.Strip(m.pages()))
	}
}

// Every page draws, and every form row explains itself.
func TestSettingsPagesDraw(t *testing.T) {
	m, _ := benchModel(140, 50)
	m.setView(placeSettings)
	for i, p := range m.settingsPages() {
		m.setSettingsPage(i)
		if body := m.dialogBody(130); len(body) < 3 {
			t.Fatalf("%s drew %d lines", p.name, len(body))
		}
		if p.form == nil {
			continue
		}
		for _, st := range flat(p.form(m)) {
			if st.line == nil && st.what == "" && st.about == nil {
				t.Errorf("%s › %s doesn't say what it does", p.name, st.label)
			}
		}
	}
}

// An agent other than the built-in one keeps what its sessions start with
// apart from Claude Code's.
func TestStartForKind(t *testing.T) {
	var d state.Dispatch
	d.SetStartFor("claude", state.Start{Model: "opus", Mode: "plan"})
	d.SetStartFor("codex", state.Start{Effort: "high", Mode: "auto"})
	if d.Model != "opus" || d.Permission != "plan" {
		t.Fatalf("Claude's start isn't in Model and Permission: %+v", d)
	}
	if got := d.StartFor("codex"); got.Effort != "high" || got.Mode != "auto" || got.Model != "" {
		t.Fatalf("codex start = %+v", got)
	}
	d.SetStartFor("codex", state.Start{})
	if _, ok := d.Starts["codex"]; ok {
		t.Fatal("an emptied start is kept")
	}
}

// Providers is one page whatever is installed: 1-9 pick a provider, enter
// goes into it and esc back, and an agent's settings open on its own.
func TestSettingsProvidersOnePage(t *testing.T) {
	m, _ := accountsModel(t)
	m.setView(placeSettings)
	if n := len(m.settingsPages()); n != pagePlugins+1 {
		t.Fatalf("%d pages: a provider has a page of its own again", n)
	}
	m.setSettingsPage(pageProviders)
	if it := m.provPicked(); it.provider != "claude" {
		t.Fatalf("Providers opens on %+v, not the first", it)
	}
	m.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	if it := m.provPicked(); it != m.provItems()[1] {
		t.Fatalf("2 picked %+v", it)
	}
	m.dialog.cursor = slices.Index(m.provItems(), provItem{provider: "zcodex"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if it := m.provPicked(); !m.dialog.inside || it.provider != "zcodex" {
		t.Fatalf("enter: inside %v on %+v", m.dialog.inside, it)
	}
	body := ansi.Strip(strings.Join(m.dialogBody(150), "\n"))
	for _, want := range []string{"Account", "one@example.com", "New sessions start with", "When a limit stops a session", "can do"} {
		if !strings.Contains(body, want) {
			t.Errorf("zcodex doesn't show %q:\n%s", want, body)
		}
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.dialog == nil || m.dialog.inside || m.provPicked().provider != "zcodex" {
		t.Fatal("esc didn't go back to the list, on the provider")
	}
	m.openAgentSettings("zplain")
	if m.dialog.page != pageProviders || !m.dialog.inside || m.provPicked().provider != "zplain" {
		t.Fatal("openAgentSettings didn't land in the agent's provider")
	}
	// Narrow, the list and what's picked take turns.
	if body := ansi.Strip(strings.Join(m.dialogBody(80), "\n")); strings.Contains(body, "│") || !strings.Contains(body, "can do") {
		t.Fatalf("narrow and inside, not the provider alone:\n%s", body)
	}
}

// Profiles makes a profile, changes its agents and what it does at a
// limit, gives it a folder, and makes it the default, all by keys.
func TestSettingsProfiles(t *testing.T) {
	m, _ := accountsModel(t)
	m.setView(placeSettings)
	m.setSettingsPage(pageProviders)
	cfg := &m.store.Config
	editing := func() (state.Profile, bool) { return cfg.ProfileNamed(m.provPicked().profile) }
	press := func(keys ...string) {
		for _, k := range keys {
			switch k {
			case "enter":
				m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			case "esc":
				m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
			case "down":
				m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
			case "right":
				m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
			default:
				for _, r := range k {
					m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
				}
			}
		}
	}
	before := len(cfg.Profiles)
	press("n", "client", "enter")
	if len(cfg.Profiles) != before+1 || !m.dialog.inside || m.provPicked().profile != "client" {
		t.Fatalf("n didn't make and open a profile: %d profiles, open %+v", len(cfg.Profiles), m.provPicked())
	}
	body := ansi.Strip(strings.Join(m.dialogBody(130), "\n"))
	for _, want := range []string{"client", "Providers", "When a limit stops a session", "Folders"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the open profile doesn't show %q:\n%s", want, body)
		}
	}
	// Add the second agent, then set it to hand off at a limit.
	p, _ := editing()
	n := len(p.Providers)
	press("down", "enter")
	if p, _ = editing(); len(p.Providers) == n {
		t.Fatalf("enter on an agent didn't add or drop it: %v", p.Providers)
	}
	if body := ansi.Strip(strings.Join(m.dialogBody(130), "\n")); !strings.Contains(body, "When the first is out") {
		t.Fatalf("with two providers, no choice of what to do when the first is out:\n%s", body)
	}
	rows := flat(m.provForm(m.provPicked()))
	for i, r := range rows {
		if r.label == "When a limit stops a session" {
			m.dialog.cursor = i
		}
		if body := ansi.Strip(strings.Join(m.dialogBody(130), "\n")); !strings.Contains(body, "When the first is out") {
			t.Fatalf("with two providers, no choice of what to do when the first is out:\n%s", body)
		}
	}
	press("right")
	if p, _ = editing(); p.Limit() != state.LimitHandoff {
		t.Fatalf("→ on the limit row: %q", p.Limit())
	}
	// A folder for it.
	for i, r := range flat(m.provForm(m.provPicked())) {
		if r.label == "+ add a folder" {
			m.dialog.cursor = i
		}
	}
	press("enter")
	m.dialog.input = []rune("~/src/client")
	press("enter")
	if r, ok := cfg.RuleFor(state.ExpandHome("~/src/client/app")); !ok || r.Profile != "client" {
		t.Fatalf("folder rule: %+v %v", r, ok)
	}
	// esc goes back to the list, not out of Settings.
	press("esc")
	if m.dialog == nil || m.dialog.inside {
		t.Fatal("esc in a profile left Settings")
	}
	m.dialog.cursor = slices.Index(m.provItems(), provItem{profile: "client"})
	press("*")
	if cfg.Default().Name != "client" {
		t.Fatalf("* didn't make it the default: %s", cfg.Default().Name)
	}
}

// An agent's own settings file shows as its adapter describes it, and a
// row changed there is saved to that file; an agent with none shows none.
func TestAgentFileSections(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := &Model{snap: &fleet.Snapshot{}, store: &state.Store{}, dialog: &dialog{}}
	secs := m.fileSections("claude")
	if len(secs) != 2 || secs[0].title != "settings.json" || secs[1].title != "Environment" {
		t.Fatalf("sections: %+v", secs)
	}
	for _, st := range secs[0].rows {
		if st.label == "Default model" {
			st.set("opus")
		}
	}
	b, err := os.ReadFile(m.dialog.settings.Path)
	if err != nil || !strings.Contains(string(b), `"model": "opus"`) {
		t.Fatalf("settings.json after a change: %s %v", b, err)
	}
	if defs := m.agentDefs("claude"); len(defs) == 0 || defs[0].Path != "" {
		t.Fatalf("the built-in definition isn't first: %+v", defs)
	}
	if secs := m.fileSections("nosuch"); secs != nil {
		t.Fatalf("an unknown agent has sections: %+v", secs)
	}
}

// tab turns the page in Efficiency and on Agents' Projects and Wall, where
// there's no list and Session to go between.
func TestTabTurnsPages(t *testing.T) {
	m, _ := benchModel(140, 50)
	m.setView(placeEff)
	p := m.eff.page
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.eff.page == p {
		t.Fatal("tab didn't turn Efficiency's page")
	}
	m.setView(placeProjects)
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.projTab() != ptWorktrees {
		t.Fatalf("tab on Projects went to page %d, not Worktrees", m.projTab())
	}
}

// An agent's models table has a row a model: what it reads, its window
// and efforts, and ? with a footnote for what the agent doesn't know.
func TestModelTable(t *testing.T) {
	rows := ansi.Strip(strings.Join(modelTable("claude", []agent.Choice{{ID: "opus"}, {ID: "haiku"}}), "\n"))
	for _, want := range []string{"\n  opus               ✓       ✓    1M       low–max\n", "\n  haiku              ✓       ✓    200k     low–max"} {
		if !strings.Contains(rows+"\n", want) {
			t.Errorf("no row %q in\n%s", want, rows)
		}
	}
	if strings.Contains(rows, "unknown to rush") {
		t.Errorf("a footnote with nothing unknown:\n%s", rows)
	}
	rows = ansi.Strip(strings.Join(modelTable("nobody", []agent.Choice{{ID: "m"}}), "\n"))
	if !strings.Contains(rows, "\n  m                  ?       ?    ?        –\n") || !strings.Contains(rows, "? unknown to rush until it's used") {
		t.Errorf("an agent that knows nothing of its model:\n%s", rows)
	}
}

// A plugin's requirements are a cell each, met here or not.
func TestNeedCells(t *testing.T) {
	r := plugin.Requires{OS: []string{runtime.GOOS, "plan9"}, Arch: []string{runtime.GOARCH}, Bin: []string{"git", "nope"}}
	got := ansi.Strip(needCells(r, []string{"nope"}))
	want := "✓ " + cmp.Or(plugin.OSNames[runtime.GOOS], runtime.GOOS) + "  – plan9  ✓ " + runtime.GOARCH + "  ✓ git on PATH  – nope on PATH"
	if got != want {
		t.Fatalf("needCells = %q, want %q", got, want)
	}
}
