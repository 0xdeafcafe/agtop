package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/secrets"
)

// fakeVault stands in for kv.
type fakeVault struct {
	existing []string
	added    map[string]string
	lsErr    error
	addErr   error
}

func (f *fakeVault) names() ([]string, error) { return f.existing, f.lsErr }

func (f *fakeVault) add(name, value string) error {
	if f.addErr != nil {
		return f.addErr
	}
	if f.added == nil {
		f.added = map[string]string{}
	}
	f.added[name] = value
	return nil
}

func withVault(t *testing.T, f *fakeVault) {
	was := vault
	vault = f
	t.Cleanup(func() { vault = was })
}

// A fake but real-looking key, built at run time.
var pastedKey = "sk-proj-" + strings.Repeat("Ab3dEf7gHi2jKlMn", 3)

// landVault runs a vault command and applies what it brings back on the UI
// goroutine, returning the command that follows (a send, not run here).
func landVault(t *testing.T, m *Model, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	if cmd == nil {
		t.Fatal("no vault command")
	}
	apply, ok := cmd().(applyMsg)
	if !ok {
		t.Fatal("the vault command brought back no applyMsg")
	}
	return apply(m)
}

func boxWithKey(t *testing.T, f *fakeVault) (*Model, *hostConn) {
	m, c := infoModel(t)
	withVault(t, f)
	c.input = []rune("deploy with " + pastedKey + " please")
	return m, c
}

func TestPastedKeyAsksToSaveIt(t *testing.T) {
	f := &fakeVault{existing: []string{"OPENAI_API_KEY"}}
	m, c := boxWithKey(t, f)
	landVault(t, m, m.sendPane(c, false))
	if m.confirm == nil || m.confirm.question != "Save as vault secret OPENAI_API_KEY_2?" {
		t.Fatalf("want the save question with a free name, got %+v", m.confirm)
	}
	if len(c.sending) != 0 {
		t.Fatal("nothing goes before you answer")
	}
	if strings.Contains(m.confirm.detail, pastedKey) || !strings.Contains(m.confirm.detail, "sk-p… 56 chars") {
		t.Errorf("the question names the key without showing it: %s", m.confirm.detail)
	}

	landVault(t, m, m.confirmKey("y"))
	if f.added["OPENAI_API_KEY_2"] != pastedKey {
		t.Fatalf("saved %v", f.added)
	}
	sent := sentText(t, c)
	want := "deploy with {{vault:OPENAI_API_KEY_2}} please\n\n" + secrets.UsageLine("OPENAI_API_KEY_2")
	if sent != want {
		t.Errorf("sent %q, want %q", sent, want)
	}
	if len(c.input) != 0 || len(c.vault.swaps) != 0 {
		t.Error("a sent box lets the vault answers go")
	}
}

func TestEnterSavesToo(t *testing.T) {
	f := &fakeVault{}
	m, c := boxWithKey(t, f)
	landVault(t, m, m.sendPane(c, false))
	landVault(t, m, m.confirmKey("enter"))
	if f.added["OPENAI_API_KEY"] != pastedKey || !strings.Contains(sentText(t, c), "{{vault:OPENAI_API_KEY}}") {
		t.Fatalf("enter should save and send: %v", f.added)
	}
}

func TestNoAndEscSendUnchanged(t *testing.T) {
	for _, key := range []string{"n", "esc"} {
		f := &fakeVault{}
		m, c := boxWithKey(t, f)
		landVault(t, m, m.sendPane(c, false))
		m.confirmKey(key)
		if len(f.added) != 0 {
			t.Errorf("%s saved %v", key, f.added)
		}
		if got := sentText(t, c); got != "deploy with "+pastedKey+" please" {
			t.Errorf("%s sent %q", key, got)
		}
	}
}

func TestFailedSaveNeverSendsOnItsOwn(t *testing.T) {
	f := &fakeVault{addErr: errors.New("the master is not running")}
	m, c := boxWithKey(t, f)
	landVault(t, m, m.sendPane(c, false))
	landVault(t, m, m.confirmKey("y"))
	if len(c.sending) != 0 {
		t.Fatal("a failed save sent the message")
	}
	if m.confirm == nil || !strings.Contains(m.confirm.detail, "the master is not running") {
		t.Fatalf("want the error shown, got %+v", m.confirm)
	}
	m.confirmKey("enter")
	if len(c.sending) != 0 || m.confirm == nil {
		t.Fatal("enter alone must not send the raw key after a failed save")
	}
	m.confirmKey("esc")
	if len(c.sending) != 0 || string(c.input) != "deploy with "+pastedKey+" please" {
		t.Fatalf("esc goes back to the message: sent %d, box %q", len(c.sending), string(c.input))
	}

	// Sent again, it asks again, and this time y sends it as it is.
	landVault(t, m, m.sendPane(c, false))
	landVault(t, m, m.confirmKey("y"))
	m.confirmKey("y")
	if got := sentText(t, c); got != "deploy with "+pastedKey+" please" {
		t.Errorf("sent %q", got)
	}
}

func TestMissingKVShowsTheError(t *testing.T) {
	f := &fakeVault{lsErr: errors.New("kv isn't installed")}
	m, c := boxWithKey(t, f)
	landVault(t, m, m.sendPane(c, false))
	if m.confirm == nil || m.confirm.question != "Couldn't save the secret to the vault" || len(c.sending) != 0 {
		t.Fatalf("want the error, got %+v", m.confirm)
	}
}

func TestTwoSecretsAskOneAfterTheOther(t *testing.T) {
	f := &fakeVault{}
	m, c := boxWithKey(t, f)
	gh := "ghp_" + strings.Repeat("Zq7Wx3Rt9", 4)
	c.input = []rune(pastedKey + " and " + gh)
	landVault(t, m, m.sendPane(c, false))
	// Saving the first sends again, which finds the second and asks.
	landVault(t, m, landVault(t, m, m.confirmKey("y")))
	if m.confirm == nil || m.confirm.question != "Save as vault secret GITHUB_TOKEN?" || len(c.sending) != 0 {
		t.Fatalf("want the second question, got %+v", m.confirm)
	}
	if box := string(c.input); strings.Contains(box, pastedKey) || !strings.HasPrefix(box, "{{vault:OPENAI_API_KEY}} and ") {
		t.Errorf("a saved secret leaves the box, so drafts never keep it: %q", box)
	}
	landVault(t, m, m.confirmKey("y"))
	sent := sentText(t, c)
	if strings.Contains(sent, pastedKey) || strings.Contains(sent, gh) ||
		!strings.HasPrefix(sent, "{{vault:OPENAI_API_KEY}} and {{vault:GITHUB_TOKEN}}") {
		t.Errorf("sent %q", sent)
	}
}

func TestOrdinaryMessageIsNotAsked(t *testing.T) {
	f := &fakeVault{}
	m, c := boxWithKey(t, f)
	c.input = []rune("set OPENAI_API_KEY=sk-1234 or <your-key> and commit 51d07b547d0a8f3e2c1b9d4a6e7f8091a2b3c4d5")
	m.sendPane(c, false)
	if m.confirm != nil || len(c.sending) != 1 {
		t.Fatalf("an ordinary message goes straight out: confirm %+v", m.confirm)
	}
}

func TestPromptAsksBeforeStarting(t *testing.T) {
	f := &fakeVault{}
	m, _ := infoModel(t)
	withVault(t, f)
	m.input = []rune("use " + pastedKey)
	landVault(t, m, m.submit())
	if m.confirm == nil || m.confirm.question != "Save as vault secret OPENAI_API_KEY?" {
		t.Fatalf("want the save question, got %+v", m.confirm)
	}
	if string(m.input) != "use "+pastedKey {
		t.Fatal("the Prompt keeps its message while you're asked")
	}
}

// A secret inside a long paste is saved the same, and the chip's text goes
// out with the reference.
func TestSecretInAPaste(t *testing.T) {
	f := &fakeVault{}
	m, c := infoModel(t)
	withVault(t, f)
	paste := "line one\nOPENAI_API_KEY=" + pastedKey + "\nline three"
	c.input = []rune("my env:\n" + c.pastes.add(paste))
	landVault(t, m, m.sendPane(c, false))
	landVault(t, m, m.confirmKey("y"))
	sent := sentText(t, c)
	if strings.Contains(sent, pastedKey) || !strings.Contains(sent, "OPENAI_API_KEY={{vault:OPENAI_API_KEY}}") {
		t.Errorf("sent %q", sent)
	}
}
