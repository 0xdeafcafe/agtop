package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/cellw"
)

func ctrlK() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl} }

func typeBar(m *Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func TestBarOpensAndGoes(t *testing.T) {
	m, _ := benchModel(120, 40)
	m.Update(ctrlK())
	if m.bar == nil {
		t.Fatal("ctrl+k at the end of the box didn't open the bar")
	}
	if !strings.Contains(m.View().Content, "❯") {
		t.Fatal("the bar isn't drawn")
	}
	typeBar(m, "clean")
	if it := m.bar.items[0]; it.title != "Machine › Cleanup" {
		t.Fatalf("top match for clean: %q", it.title)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.bar != nil || m.view != placeMachine || m.mode != modeCleanup {
		t.Fatalf("enter didn't go to Cleanup: view %d mode %d", m.view, m.mode)
	}
	// Back returns to the agent it came from.
	m.Update(ctrlK())
	typeBar(m, "back")
	if m.bar.items[0].title != "Back" {
		t.Fatalf("no way back: %q", m.bar.items[0].title)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.view != placeAgents || m.mode != modeList {
		t.Fatalf("back didn't return to Agents: view %d", m.view)
	}
}

func TestBarCtrlKStillCuts(t *testing.T) {
	m, _ := benchModel(120, 40)
	m.host.back = 5 // the cursor has text after it
	m.Update(ctrlK())
	if m.bar != nil {
		t.Fatal("ctrl+k opened the bar instead of cutting to the line's end")
	}
}

func TestBarFindsAnAgentAndATurn(t *testing.T) {
	m, _ := benchModel(120, 40)
	m.Update(ctrlK())
	typeBar(m, "number 7")
	var found bool
	for _, it := range m.bar.items {
		if it.section == "Agents" && it.title == "agent number 7 doing things" {
			found = true
		}
	}
	if !found {
		t.Fatal("agent 7 isn't offered")
	}
	// An empty query lists the open conversation's turns, latest first.
	m.closeBar()
	m.Update(ctrlK())
	var turns []string
	for _, it := range m.bar.items {
		if strings.HasPrefix(it.section, "This session") {
			turns = append(turns, it.meta)
		}
	}
	if len(turns) == 0 || !strings.HasSuffix(turns[0], "#301") {
		t.Fatalf("turns: %v", turns)
	}
	for i, it := range m.bar.items {
		if strings.HasPrefix(it.section, "This session") && strings.HasSuffix(it.meta, "#290") {
			m.bar.cursor = i
		}
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.host.sel != "t290" || !m.host.open["t290"] {
		t.Fatalf("didn't open turn 290: sel %q", m.host.sel)
	}
	// #250 goes straight to turn 250.
	m.Update(ctrlK())
	typeBar(m, "#250")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.host.sel != "t250" || !m.host.open["t250"] {
		t.Fatalf("didn't open turn 250: sel %q", m.host.sel)
	}
}

// barTranscriptsFixture gives five agents transcripts, one of them with
// "zebracorn" in it, the third.
func barTranscriptsFixture(t *testing.T, m *Model) {
	t.Helper()
	dir := t.TempDir()
	line := `{"type":"user","timestamp":"2026-09-23T20:00:00Z","cwd":"/work","message":{"role":"user","content":"rotate the zebracorn keys"},"origin":{"kind":"human"}}`
	for i, a := range m.snap.Agents[1:6] {
		p := filepath.Join(dir, a.ID+".jsonl")
		text := "nothing to see"
		if i == 2 {
			text = line
		}
		if err := os.WriteFile(p, []byte(text+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		a.TranscriptPath = p
	}
}

// runSearch runs a transcript search started by cmd (the search batched with
// the frame's glint) to the end.
func runSearch(m *Model, cmd tea.Cmd) {
	cmd = cmd().(tea.BatchMsg)[0]
	for cmd != nil {
		cmd, _ = m.barMsg(cmd())
	}
}

func barHit(m *Model) *barItem {
	for i, it := range m.bar.items {
		if it.section == "Transcripts" {
			return &m.bar.items[i]
		}
	}
	return nil
}

func TestBarSearchesTranscripts(t *testing.T) {
	m, _ := benchModel(120, 40)
	barTranscriptsFixture(t, m)
	m.Update(ctrlK())
	typeBar(m, "zebracorn")
	cmd, _ := m.barMsg(barPauseMsg(m.bar.gen))
	runSearch(m, cmd)
	if !m.bar.done || m.bar.searched != 5 {
		t.Fatalf("searched %d of %d, done %v", m.bar.searched, m.bar.toSearch, m.bar.done)
	}
	hit := barHit(m)
	if hit == nil || !strings.Contains(hit.title, "zebracorn") {
		t.Fatalf("no transcript hit: %+v", m.bar.items)
	}
	if out := m.View().Content; !strings.Contains(out, "zebracorn") || strings.Contains(out, "search transcripts") {
		t.Fatal("the hit isn't drawn, or the on-demand hint is")
	}
	// Going there selects the agent and waits for its conversation.
	hit.run(m)
	if m.sel != m.snap.Agents[3].Key || m.jump == nil {
		t.Fatalf("went to %q, jump %v", m.sel, m.jump)
	}
}

// With SearchTranscriptsOnKey, typing matches names and commands only, and
// ctrl+enter (ctrl+j where the terminal can't tell it from enter) searches
// the transcripts.
func TestBarSearchesTranscriptsOnKey(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEnter, Mod: tea.ModCtrl}, {Code: 'j', Mod: tea.ModCtrl}} {
		m, _ := benchModel(120, 40)
		m.store.Config.SearchTranscriptsOnKey = true
		barTranscriptsFixture(t, m)
		m.Update(ctrlK())
		typeBar(m, "zebracorn")
		if cmd := m.barChanged(); cmd != nil {
			t.Fatal("typing scheduled a transcript search")
		}
		if m.bar.search != nil || barHit(m) != nil {
			t.Fatal("the transcripts were searched while typing")
		}
		out := ansi.Strip(m.View().Content)
		if !strings.Contains(out, "ctrl+j search transcripts") || m.barStatus() != "" {
			t.Fatalf("no hint for the key, or a pending search shown:\n%s", out)
		}
		m.Update(tea.KeyboardEnhancementsMsg{Flags: 1})
		if out := ansi.Strip(m.View().Content); !strings.Contains(out, "ctrl+enter search transcripts") {
			t.Fatalf("a terminal that tells ctrl+enter apart should be offered it:\n%s", out)
		}
		if key.String() != "ctrl+enter" && key.String() != "ctrl+j" {
			t.Fatalf("key reads as %q", key.String())
		}
		cmd := m.key(key)
		if cmd == nil || m.bar.search == nil {
			t.Fatalf("%s didn't search", key.String())
		}
		runSearch(m, cmd)
		if hit := barHit(m); !m.bar.done || m.bar.searched != 5 || hit == nil || !strings.Contains(hit.title, "zebracorn") {
			t.Fatalf("%s: searched %d, hit %v", key.String(), m.bar.searched, hit)
		}
		if strings.Contains(ansi.Strip(m.View().Content), "search transcripts") {
			t.Fatal("the hint stays once the search ran")
		}
	}
}

func TestFuzzy(t *testing.T) {
	cases := []struct {
		q, s string
		ok   bool
	}{
		{"mc", "Machine › Cleanup", true},
		{"setacc", "Settings › Accounts", true},
		{"zz", "Settings › Accounts", false},
		{"clean", "Machine › Cleanup", true},
	}
	for _, c := range cases {
		if _, _, ok := fuzzy(c.q, c.s); ok != c.ok {
			t.Errorf("fuzzy(%q, %q) = %v", c.q, c.s, ok)
		}
	}
	// A word start beats the middle of a word.
	a, _, _ := fuzzy("proc", "Machine › Processes")
	b, _, _ := fuzzy("proc", "reprocess")
	if a <= b {
		t.Errorf("word start %d should beat mid-word %d", a, b)
	}
}

// The frame, its labels and its shadow keep every row the screen's width.
func TestBarFrameWidth(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {130, 40}, {250, 70}} {
		m, _ := benchModel(sz[0], sz[1])
		m.Update(ctrlK())
		m.bar.search, m.bar.toSearch, m.bar.glint = &barSearch{}, 80, 7
		for i, l := range strings.Split(m.View().Content, "\n") {
			if w := cellw.String(ansi.Strip(l)); w != sz[0] {
				t.Errorf("%dx%d row %d is %d wide", sz[0], sz[1], i, w)
			}
		}
	}
}

func ctrlF() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl} }

func sections(m *Model) map[string]int {
	out := map[string]int{}
	for _, it := range m.bar.items {
		out[it.section]++
	}
	return out
}

// ctrl+f finds where you are and widens each time; ctrl+k is everywhere.
func TestBarScopes(t *testing.T) {
	m, _ := benchModel(120, 40)
	m.Update(ctrlF())
	if m.bar == nil || m.bar.scope().kind != "chat" {
		t.Fatal("ctrl+f in a Session didn't find in the chat")
	}
	typeBar(m, "streaming")
	for sec := range sections(m) {
		if !strings.HasPrefix(sec, "This chat") {
			t.Fatalf("finding in the chat showed %q", sec)
		}
	}
	m.Update(ctrlF())
	if sc := m.bar.scope(); sc.kind != "group" || sc.group != m.groupOf[m.sel] {
		t.Fatalf("second ctrl+f: %+v", sc)
	}
	m.Update(ctrlF())
	if m.bar.scope().kind != "agents" {
		t.Fatalf("third ctrl+f: %+v", m.bar.scope())
	}
	m.Update(ctrlK())
	if m.bar.scope().kind != "" {
		t.Fatal("ctrl+k in the bar didn't go to everywhere")
	}
	m.Update(ctrlF())
	if m.bar.scope().kind != "chat" {
		t.Fatal("ctrl+f from everywhere should come round to the chat")
	}
	// Backspace with nothing typed widens too.
	m.bar.query, m.bar.pos = nil, 0
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.bar.scope().kind != "group" {
		t.Fatalf("backspace on empty: %+v", m.bar.scope())
	}
	m.Update(ctrlK())
	m.Update(ctrlK())
	if m.bar != nil {
		t.Fatal("ctrl+k at everywhere should close")
	}
}

func TestBarFindInGroup(t *testing.T) {
	m, _ := benchModel(120, 40)
	m.paneFocus = false
	m.sel = m.snap.Agents[2].Key // needs you
	group := m.groupOf[m.sel]
	m.Update(ctrlF())
	if sc := m.bar.scope(); sc.kind != "group" || sc.group != group {
		t.Fatalf("ctrl+f in the list: %+v", sc)
	}
	for _, it := range m.bar.items {
		if it.section != group {
			t.Fatalf("row from %q while finding in %q", it.section, group)
		}
	}
	if len(m.bar.items) == 0 {
		t.Fatal("the group's agents aren't listed")
	}
	// In Machine it finds among Machine's pages.
	m.closeBar()
	m.setView(placeMachine)
	m.Update(ctrlF())
	if sc := m.bar.scope(); sc.kind != "place" || len(m.bar.items) != len(machinePages) {
		t.Fatalf("ctrl+f in Machine: %+v %d rows", sc, len(m.bar.items))
	}
}

func TestBarStartsAnAgent(t *testing.T) {
	m, _ := benchModel(120, 40)
	m.Update(ctrlK())
	if it := m.bar.items[0]; it.title != "Start an agent" {
		t.Fatalf("untyped, the first row is %q, not Start an agent", it.title)
	}
	typeBar(m, "zqxj fix the flaky test")
	var start *barItem
	for i, it := range m.bar.items {
		if it.section == "Start" {
			start = &m.bar.items[i]
		}
	}
	if start == nil || start.title != "zqxj fix the flaky test" {
		t.Fatalf("what's typed isn't offered as a new agent's task: %+v", start)
	}
	m.closeBar()
	m.Update(ctrlK())
	typeBar(m, "#done")
	for _, it := range m.bar.items {
		if it.section == "Start" {
			t.Fatal("a # command is offered as a task")
		}
	}
	m.closeBar()
	m.paneFocus = true
	m.Update(ctrlK())
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.bar != nil || m.paneFocus || m.view != 0 || m.inKind != inPrompt {
		t.Fatalf("Start an agent didn't put the keys in the Prompt: paneFocus %v view %d", m.paneFocus, m.view)
	}
}

// Settings › General switches how ctrl+k searches the transcripts.
func TestBarSearchSetting(t *testing.T) {
	m, _ := benchModel(120, 40)
	find := func() setting {
		for _, s := range m.generalSettings() {
			if s.label == "ctrl+k searches transcripts" {
				return s
			}
		}
		t.Fatal("no setting for it")
		return setting{}
	}
	if s := find(); s.value != "as you type" || m.store.Config.SearchTranscriptsOnKey {
		t.Fatalf("default = %q", s.value)
	}
	cycle(find(), 1)
	if s := find(); s.value != "on ctrl+enter" || !m.store.Config.SearchTranscriptsOnKey {
		t.Fatalf("after a change = %q", s.value)
	}
}
