package ui

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/bundled/kanbanvault"
	"github.com/0xdeafcafe/rush/internal/bundled/kanbanvault/secrets"
	"github.com/0xdeafcafe/rush/internal/hooks"
	"github.com/0xdeafcafe/rush/internal/jsonx"
	"github.com/0xdeafcafe/rush/internal/plugin"
)

// fakeVault stands in for Kanban Code's vault behind the kanban-vault
// plugin.
type fakeVault struct {
	mu     sync.Mutex
	added  map[string]string
	lsErr  error
	addErr error
}

func (f *fakeVault) Names(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var names []string
	for n := range f.added {
		names = append(names, n)
	}
	return names, f.lsErr
}

func (f *fakeVault) Add(_ context.Context, name, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.addErr != nil {
		return f.addErr
	}
	if f.added == nil {
		f.added = map[string]string{}
	}
	f.added[name] = value
	return nil
}

func (f *fakeVault) saved(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.added[name]
}

// vaultHooks are a window's plugins: kanban-vault over v, behind a broker
// that does what plugind does with one plugin's answers.
func vaultHooks(t *testing.T, v kanbanvault.Vault) *hooks.Client {
	t.Helper()
	h := kanbanvault.Handler(v)
	c := hooks.Over(plugin.UIState{Plugins: []plugin.UIPlugin{{Name: kanbanvault.Name, UI: kanbanvault.Manifest.UI}}},
		func(ctx context.Context, method string, params jsontext.Value) (any, error) {
			if method != "ui.intercept" && method != "ui.intercept.answer" {
				return map[string]any{}, nil
			}
			var in plugin.Intercept
			_ = jsonx.Unmarshal(params, &in)
			out, err := h(ctx, method, params)
			if err != nil {
				return nil, err
			}
			r := out.(plugin.InterceptResult)
			if r.Action == "ask" {
				if err := plugin.CleanAsk(&r); err != nil {
					return nil, err
				}
			}
			if r.Action == "ask" || r.Action == "rewrite" {
				r.Text = r.Apply(in.Text)
			}
			r.Plugin = kanbanvault.Name
			return r, nil
		})
	c.Start()
	t.Cleanup(c.Close)
	for deadline := time.Now().Add(2 * time.Second); !c.Intercepts(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the window never reached its plugins")
		}
	}
	return c
}

// A fake but real-looking key, built at run time.
var pastedKey = "sk-proj-" + strings.Repeat("Ab3dEf7gHi2jKlMn", 3)

// land runs cmd, the plugins' say, and hands what it brings back to the
// window, returning what follows.
func land(t *testing.T, m *Model, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	if cmd == nil {
		t.Fatal("nothing went to the plugins")
	}
	msg, ok := cmd().(interceptedMsg)
	if !ok {
		t.Fatal("the plugins' say didn't come back")
	}
	return m.onIntercepted(msg)
}

func boxWithKey(t *testing.T, v *fakeVault) (*Model, *hostConn) {
	m, c := infoModel(t)
	m.hooks = vaultHooks(t, v)
	c.input = []rune("deploy with " + pastedKey + " please")
	return m, c
}

func TestPastedKeyAsksToSaveIt(t *testing.T) {
	v := &fakeVault{}
	m, c := boxWithKey(t, v)
	land(t, m, m.sendPane(c, false))
	if m.confirm == nil || m.confirm.question != "Save as vault secret OPENAI_API_KEY?" {
		t.Fatalf("want the save question, got %+v", m.confirm)
	}
	if len(c.sending) != 0 {
		t.Fatal("nothing goes before you answer")
	}
	if strings.Contains(m.confirm.detail, pastedKey) || !strings.Contains(m.confirm.detail, "sk-p… 56 chars") ||
		!strings.Contains(m.confirm.detail, "it goes as {{vault:OPENAI_API_KEY}} and agents use it through kv run") {
		t.Errorf("the question names the key without showing it: %s", m.confirm.detail)
	}
	if keys := stripAnsi(m.confirm.keys()); keys != "y save it   n/esc send as is   ctrl+c cancel" {
		t.Errorf("keys %q", keys)
	}

	land(t, m, m.confirmKey("y"))
	if v.saved("OPENAI_API_KEY") != pastedKey {
		t.Fatalf("saved %v", v.added)
	}
	want := "deploy with {{vault:OPENAI_API_KEY}} please\n\n" + secrets.UsageLine("OPENAI_API_KEY")
	if sent := sentText(t, c); sent != want {
		t.Errorf("sent %q, want %q", sent, want)
	}
	if len(c.input) != 0 || c.intercepting {
		t.Error("a sent box is empty and no longer asked about")
	}
}

func TestEnterSavesToo(t *testing.T) {
	v := &fakeVault{}
	m, c := boxWithKey(t, v)
	land(t, m, m.sendPane(c, false))
	land(t, m, m.confirmKey("enter"))
	if v.saved("OPENAI_API_KEY") != pastedKey || !strings.Contains(sentText(t, c), "{{vault:OPENAI_API_KEY}}") {
		t.Fatalf("enter should save and send: %v", v.added)
	}
}

func TestNoAndEscSendUnchanged(t *testing.T) {
	for _, key := range []string{"n", "esc"} {
		v := &fakeVault{}
		m, c := boxWithKey(t, v)
		land(t, m, m.sendPane(c, false))
		land(t, m, m.confirmKey(key))
		if len(v.added) != 0 {
			t.Errorf("%s saved %v", key, v.added)
		}
		if got := sentText(t, c); got != "deploy with "+pastedKey+" please" {
			t.Errorf("%s sent %q", key, got)
		}
	}
}

func TestCtrlCCancelsTheSend(t *testing.T) {
	v := &fakeVault{}
	m, c := boxWithKey(t, v)
	land(t, m, m.sendPane(c, false))
	if cmd := m.confirmKey("ctrl+c"); cmd != nil || m.confirm != nil {
		t.Fatal("ctrl+c closes the question and asks nothing more")
	}
	if len(c.sending) != 0 || string(c.input) != "deploy with "+pastedKey+" please" || c.intercepting || len(v.added) != 0 {
		t.Fatalf("ctrl+c leaves the box: sent %d, box %q", len(c.sending), string(c.input))
	}
}

func TestFailedSaveNeverSendsOnItsOwn(t *testing.T) {
	v := &fakeVault{addErr: errors.New("the master is not running")}
	m, c := boxWithKey(t, v)
	land(t, m, m.sendPane(c, false))
	land(t, m, m.confirmKey("y"))
	if len(c.sending) != 0 {
		t.Fatal("a failed save sent the message")
	}
	if m.confirm == nil || m.confirm.question != "Couldn't save the secret to the vault" || !strings.Contains(m.confirm.detail, "the master is not running") {
		t.Fatalf("want the error shown, got %+v", m.confirm)
	}
	if m.confirmKey("enter"); len(c.sending) != 0 || m.confirm == nil {
		t.Fatal("enter alone must not send the raw key after a failed save")
	}
	land(t, m, m.confirmKey("esc"))
	if len(c.sending) != 0 || string(c.input) != "deploy with "+pastedKey+" please" {
		t.Fatalf("esc goes back to the message: sent %d, box %q", len(c.sending), string(c.input))
	}

	// Sent again, it asks again, and this time y sends it as it is.
	land(t, m, m.sendPane(c, false))
	land(t, m, m.confirmKey("y"))
	land(t, m, m.confirmKey("y"))
	if got := sentText(t, c); got != "deploy with "+pastedKey+" please" {
		t.Errorf("sent %q", got)
	}
}

func TestTwoSecretsAskOneAfterTheOther(t *testing.T) {
	v := &fakeVault{}
	m, c := boxWithKey(t, v)
	gh := "ghp_" + strings.Repeat("Zq7Wx3Rt9", 4)
	c.input = []rune(pastedKey + " and " + gh)
	land(t, m, m.sendPane(c, false))
	land(t, m, m.confirmKey("y"))
	if m.confirm == nil || m.confirm.question != "Save as vault secret GITHUB_TOKEN?" || len(c.sending) != 0 {
		t.Fatalf("want the second question, got %+v", m.confirm)
	}
	if box := string(c.input); strings.Contains(box, pastedKey) || !strings.HasPrefix(box, "{{vault:OPENAI_API_KEY}} and ") {
		t.Errorf("a saved secret leaves the box, so drafts never keep it: %q", box)
	}
	land(t, m, m.confirmKey("y"))
	sent := sentText(t, c)
	if strings.Contains(sent, pastedKey) || strings.Contains(sent, gh) ||
		!strings.HasPrefix(sent, "{{vault:OPENAI_API_KEY}} and {{vault:GITHUB_TOKEN}}") ||
		!strings.Contains(sent, secrets.UsageLine("OPENAI_API_KEY")+"\n"+secrets.UsageLine("GITHUB_TOKEN")) {
		t.Errorf("sent %q", sent)
	}
}

func TestOrdinaryMessageIsNotAsked(t *testing.T) {
	v := &fakeVault{}
	m, c := boxWithKey(t, v)
	c.input = []rune("set OPENAI_API_KEY=sk-1234 or <your-key> and commit 51d07b547d0a8f3e2c1b9d4a6e7f8091a2b3c4d5")
	land(t, m, m.sendPane(c, false))
	if m.confirm != nil || len(c.sending) != 1 {
		t.Fatalf("an ordinary message goes straight out: confirm %+v", m.confirm)
	}
}

func TestPromptAsksBeforeStarting(t *testing.T) {
	v := &fakeVault{}
	m, _ := infoModel(t)
	m.hooks = vaultHooks(t, v)
	m.input = []rune("use " + pastedKey)
	land(t, m, m.submit())
	if m.confirm == nil || m.confirm.question != "Save as vault secret OPENAI_API_KEY?" {
		t.Fatalf("want the save question, got %+v", m.confirm)
	}
	if string(m.input) != "use "+pastedKey {
		t.Fatal("the Prompt keeps its message while you're asked")
	}
	cmd := m.confirmKey("y")
	if !m.promptIntercepting || m.submit() != nil {
		t.Fatal("the Prompt doesn't send while the plugin acts on the answer")
	}
	msg := cmd().(interceptedMsg)
	if !msg.prompt || msg.r.Action != "rewrite" {
		t.Fatalf("y came back as %+v", msg)
	}
	m.onIntercepted(msg)
	if v.saved("OPENAI_API_KEY") != pastedKey || len(m.input) != 0 || m.promptIntercepting {
		t.Fatalf("saved %v, Prompt %q", v.added, string(m.input))
	}
}

// A secret inside a long paste is saved the same: the chip stays a chip,
// and its text goes out with the reference.
func TestSecretInAPaste(t *testing.T) {
	v := &fakeVault{}
	m, c := infoModel(t)
	m.hooks = vaultHooks(t, v)
	paste := "line one\nOPENAI_API_KEY=" + pastedKey + "\nline three"
	c.input = []rune("my env:\n" + c.pastes.add(paste))
	chip := string(c.input)
	land(t, m, m.sendPane(c, false))
	msg := m.confirmKey("y")().(interceptedMsg)
	// The change goes into the box in place, so the chip stays a chip.
	b, _ := m.interceptBox(msg)
	b.rewrite(msg.r)
	if !strings.HasPrefix(string(c.input), chip) || len(c.pastes.text) != 1 {
		t.Fatalf("box %q, pastes %d", string(c.input), len(c.pastes.text))
	}
	for _, p := range c.pastes.text {
		if strings.Contains(p, pastedKey) || !strings.Contains(p, "{{vault:OPENAI_API_KEY}}") {
			t.Fatalf("paste %q", p)
		}
	}
	msg.was, msg.r = string(c.input), plugin.InterceptResult{Action: "allow"}
	m.onIntercepted(msg)
	sent := sentText(t, c)
	if strings.Contains(sent, pastedKey) || !strings.Contains(sent, "OPENAI_API_KEY={{vault:OPENAI_API_KEY}}") {
		t.Errorf("sent %q", sent)
	}
}
