package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/host"
	"github.com/0xdeafcafe/agtop/internal/state"
	"github.com/0xdeafcafe/agtop/internal/statusline"
)

func barAgentFixture(t *testing.T) (*Model, *fleet.Agent, *hostConn) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	m := &Model{snap: &fleet.Snapshot{}, store: &state.Store{}, w: 160, h: 44}
	a := &fleet.Agent{Key: "k", DisplayName: "fixer", Cwd: "/src/agtop", Branch: "main", Acct: claude.Account{Name: "work", ConfigDir: t.TempDir()}}
	c := &hostConn{key: "k", sess: convo.New(), open: map[string]bool{}, client: &host.Client{}}
	c.sess.Info = host.Info{Model: "claude-opus-5-5", Effort: "high", PermissionMode: "auto", CostUSD: 8.18, Queue: []string{"later"}}
	c.sess.Context = 170_000
	return m, a, c
}

// The agent header is drawn from its layout: by default what it always
// showed; moved about in /statusline, the real header follows as you go.
func TestAgentHeaderFollowsItsLayout(t *testing.T) {
	m, a, c := barAgentFixture(t)
	head := func() (string, string) {
		h := m.paneHeader(a, c, 160)
		return ansi.Strip(h[0]), ansi.Strip(h[1])
	}
	r1, r2 := head()
	for _, want := range []string{"fixer", "ctx", "$8.18"} {
		if !strings.Contains(r1, want) {
			t.Fatalf("line 1 lacks %q: %q", want, r1)
		}
	}
	for _, want := range []string{"/src/agtop · main", "high · auto", "connected"} {
		if !strings.Contains(r2, want) {
			t.Fatalf("line 2 lacks %q: %q", want, r2)
		}
	}
	if strings.Contains(r1+r2, "queued") {
		t.Fatal("the queue isn't shown until you add it")
	}

	m.openStatusLine(c, a)
	st := m.sheet.(*statusSheet)
	if st.tab != stAgent {
		t.Fatal("from an agent, the sheet opens on its header")
	}
	// Add the queue: it goes on the last line in use, and the header shows
	// it before anything's saved.
	for i, sl := range st.slots() {
		if sl.id == "queue" {
			st.cur = i
		}
	}
	st.key(m, tea.KeyPressMsg{}, "space")
	if _, r2 := head(); !strings.Contains(r2, "1 queued") {
		t.Fatalf("live: %q", r2)
	}
	// Esc: nothing was saved, and the header is as it was.
	st.key(m, tea.KeyPressMsg{}, "esc")
	if _, r2 := head(); strings.Contains(r2, "queued") {
		t.Fatalf("after esc: %q", r2)
	}
}

// On a narrow screen, the segments last on a line go first; an agent's
// name and state stay.
func TestAgentHeaderNarrowDropsTheLast(t *testing.T) {
	m, a, c := barAgentFixture(t)
	wide := ansi.Strip(m.paneHeader(a, c, 160)[1])
	narrow := ansi.Strip(m.paneHeader(a, c, 44)[1])
	if !strings.Contains(wide, "auto") || strings.Contains(narrow, "auto") || !strings.Contains(narrow, "/src/agtop") {
		t.Fatalf("wide %q\nnarrow %q", wide, narrow)
	}
	if !strings.Contains(ansi.Strip(m.paneHeader(a, c, 44)[0]), "fixer") {
		t.Fatal("the name always shows")
	}
}

// Saving keeps agtop's own lines, and leaves Claude Code's settings alone
// when its line wasn't touched.
func TestSaveBarsLeavesClaudeAlone(t *testing.T) {
	m, a, c := barAgentFixture(t)
	settings := filepath.Join(a.Acct.ConfigDir, "settings.json")
	os.WriteFile(settings, []byte(`{"model":"opus"}`), 0o600)
	m.openStatusLine(c, a)
	st := m.sheet.(*statusSheet)
	st.key(m, tea.KeyPressMsg{}, "tab") // the top bar
	if st.tab != stTop {
		t.Fatalf("tab went to %d", st.tab)
	}
	st.key(m, tea.KeyPressMsg{}, "r")
	st.cur = 0 // "today", first on line 1
	st.key(m, tea.KeyPressMsg{}, "space")
	st.key(m, tea.KeyPressMsg{}, "enter")
	if m.sheet != nil {
		t.Fatalf("still open: %s", st.err)
	}
	got := statusline.LoadBars()
	if got.Top.Shown("today") || !got.Top.Shown("usage") || !got.Agent.Shown("folder") || m.bars.Top.Shown("today") {
		t.Fatalf("saved %+v", got)
	}
	if b, _ := os.ReadFile(settings); strings.Contains(string(b), "statusLine") {
		t.Fatalf("settings.json touched: %s", b)
	}
	text := ansi.Strip(strings.Join(m.header(), "\n"))
	if strings.Contains(text, "today") {
		t.Fatalf("top bar still shows today:\n%s", text)
	}
}

// #statusline from the list opens on the top bar, with no agent needed.
func TestHashStatuslineOpensTheTopBar(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := &Model{snap: &fleet.Snapshot{}, store: &state.Store{}, w: 160, h: 44}
	m.command(nil, "#statusline")
	st, ok := m.sheet.(*statusSheet)
	if !ok || st.tab != stTop {
		t.Fatalf("sheet %T", m.sheet)
	}
	if text := ansi.Strip(strings.Join(st.body(m, 108, 40), "\n")); !strings.Contains(text, "Spend today") || !strings.Contains(text, "enter save") {
		t.Fatalf("top bar tab:\n%s", text)
	}
}

// A line with no room for everything ends ⋯, and /statusline names what
// was left out.
func TestBarNoRoomIsShown(t *testing.T) {
	m, a, c := barAgentFixture(t)
	c.sess.Info.Queue = nil
	narrow := ansi.Strip(m.paneHeader(a, c, 44)[1])
	if !strings.HasSuffix(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(narrow), "● connected")), "⋯") {
		t.Fatalf("narrow line 2 should end ⋯: %q", narrow)
	}
	if d := m.barDropped(barAgent); !d["mode"] || d["folder"] {
		t.Fatalf("dropped %v", d)
	}
	if wide := ansi.Strip(m.paneHeader(a, c, 160)[1]); strings.Contains(wide, "⋯") {
		t.Fatalf("wide has room: %q", wide)
	}
	// A narrow builder (no pane behind it here): what its preview left out
	// is flagged.
	m.openStatusLine(c, a)
	raw := strings.Join(m.sheet.body(m, 56, 40), "\n")
	text := ansi.Strip(raw)
	if !strings.Contains(raw, paint(cYellow, "■")) || !strings.Contains(text, "hasn't room") {
		t.Fatalf("builder:\n%s", text)
	}
}
