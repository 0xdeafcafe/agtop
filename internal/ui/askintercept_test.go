package ui

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/hooks"
	"github.com/0xdeafcafe/rush/internal/jsonx"
	"github.com/0xdeafcafe/rush/internal/plugin"
)

// askPlugin is a plugin that asks before a message holding a token
// goes: y stores the token and puts a reference in its place, with a line
// on using it at the end; n lets the token go as it is. A failed store
// asks again, whether to send it as it is.
type askPlugin struct {
	mu     sync.Mutex
	stored map[string]string
	addErr error
	asked  map[string]askQ
	letGo  map[string]bool
	n      int
}

type askQ struct {
	value, name string
	failed      bool
}

var tokenRE = regexp.MustCompile(`sk-proj-[A-Za-z0-9]{40,}|ghp_[A-Za-z0-9]{36}`)

func askRef(name string) string { return "{{secret:" + name + "}}" }

func askUsage(name string) string { return "(" + askRef(name) + " is a stored secret.)" }

func (f *askPlugin) saved(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stored[name]
}

func (f *askPlugin) next(text string) (value, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, v := range tokenRE.FindAllString(text, -1) {
		if f.letGo[v] {
			continue
		}
		if strings.HasPrefix(v, "ghp_") {
			return v, "GITHUB_TOKEN"
		}
		return v, "OPENAI_API_KEY"
	}
	return "", ""
}

func (f *askPlugin) ask(q askQ, changes plugin.InterceptResult) plugin.InterceptResult {
	f.mu.Lock()
	f.n++
	id := strconv.Itoa(f.n)
	if f.asked == nil {
		f.asked = map[string]askQ{}
	}
	f.asked[id] = q
	f.mu.Unlock()
	changes.Action, changes.ID = "ask", id
	if q.failed {
		changes.Question, changes.Detail = "Couldn't store the secret", f.addErr.Error()
		changes.Choices = []plugin.AskChoice{{Key: "y", Label: "send as is"}, {Key: "n", Label: "back to the box", Esc: true}}
		return changes
	}
	changes.Question = "Save as secret " + q.name + "?"
	changes.Detail = fmt.Sprintf("your message has a token (%s… %d chars) · saved, it goes as %s", q.value[:4], len(q.value), askRef(q.name))
	changes.Choices = []plugin.AskChoice{{Key: "y", Label: "save it", Enter: true}, {Key: "n", Label: "send as is", Esc: true}}
	return changes
}

// then is changes to text, asking about the next token left in it.
func (f *askPlugin) then(text string, changes plugin.InterceptResult) plugin.InterceptResult {
	if v, name := f.next(changes.Apply(text)); v != "" {
		return f.ask(askQ{value: v, name: name}, changes)
	}
	if len(changes.Replace) == 0 && changes.Append == "" {
		return plugin.InterceptResult{Action: "allow"}
	}
	changes.Action = "rewrite"
	return changes
}

func (f *askPlugin) handle(_ context.Context, method string, params jsontext.Value) (any, error) {
	switch method {
	case "ui.intercept":
		var in plugin.Intercept
		_ = jsonx.Unmarshal(params, &in)
		return f.then(in.Text, plugin.InterceptResult{}), nil
	case "ui.intercept.answer":
		var in plugin.InterceptAnswer
		_ = jsonx.Unmarshal(params, &in)
		f.mu.Lock()
		q := f.asked[in.ID]
		delete(f.asked, in.ID)
		f.mu.Unlock()
		switch {
		case q.failed && in.Key == "y", !q.failed && in.Key == "n":
			f.mu.Lock()
			if f.letGo == nil {
				f.letGo = map[string]bool{}
			}
			f.letGo[q.value] = true
			f.mu.Unlock()
			return f.then(in.Text, plugin.InterceptResult{}), nil
		case q.failed:
			return plugin.InterceptResult{Action: "block", Reason: "not sent; it's still in the box"}, nil
		}
		if f.addErr != nil {
			q.failed = true
			return f.ask(q, plugin.InterceptResult{}), nil
		}
		f.mu.Lock()
		if f.stored == nil {
			f.stored = map[string]string{}
		}
		f.stored[q.name] = q.value
		f.mu.Unlock()
		replaced := strings.ReplaceAll(in.Text, q.value, askRef(q.name))
		sep := "\n\n"
		if last := replaced[strings.LastIndexByte(replaced, '\n')+1:]; strings.HasPrefix(last, "({{secret:") {
			sep = "\n"
		}
		return f.then(in.Text, plugin.InterceptResult{Replace: []plugin.Replacement{{Old: q.value, New: askRef(q.name)}}, Append: sep + askUsage(q.name)}), nil
	}
	return map[string]any{}, nil
}

// askHooks are a window's plugins: f, behind a broker that does what
// plugind does with one plugin's answers.
func askHooks(t *testing.T, f *askPlugin) *hooks.Client {
	t.Helper()
	const name = "secrets"
	c := hooks.Over(plugin.UIState{Plugins: []plugin.UIPlugin{{Name: name, UI: []string{plugin.UIInput, plugin.UIIntercept}}}},
		func(ctx context.Context, method string, params jsontext.Value) (any, error) {
			if method != "ui.intercept" && method != "ui.intercept.answer" {
				return map[string]any{}, nil
			}
			var in plugin.Intercept
			_ = jsonx.Unmarshal(params, &in)
			out, err := f.handle(ctx, method, params)
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
			r.Plugin = name
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

func boxWithKey(t *testing.T, v *askPlugin) (*Model, *hostConn) {
	m, c := infoModel(t)
	m.hooks = askHooks(t, v)
	c.input = []rune("deploy with " + pastedKey + " please")
	return m, c
}

func TestPastedKeyAsksToSaveIt(t *testing.T) {
	v := &askPlugin{}
	m, c := boxWithKey(t, v)
	land(t, m, m.sendPane(c, false))
	if m.confirm == nil || m.confirm.question != "Save as secret OPENAI_API_KEY?" {
		t.Fatalf("want the save question, got %+v", m.confirm)
	}
	if len(c.sending) != 0 {
		t.Fatal("nothing goes before you answer")
	}
	if strings.Contains(m.confirm.detail, pastedKey) || !strings.Contains(m.confirm.detail, "sk-p… 56 chars") ||
		!strings.Contains(m.confirm.detail, "it goes as {{secret:OPENAI_API_KEY}}") {
		t.Errorf("the question names the key without showing it: %s", m.confirm.detail)
	}
	if keys := stripAnsi(m.confirm.keys()); keys != "y save it   n/esc send as is   ctrl+c cancel" {
		t.Errorf("keys %q", keys)
	}

	land(t, m, m.confirmKey("y"))
	if v.saved("OPENAI_API_KEY") != pastedKey {
		t.Fatalf("saved %v", v.stored)
	}
	want := "deploy with {{secret:OPENAI_API_KEY}} please\n\n" + askUsage("OPENAI_API_KEY")
	if sent := sentText(t, c); sent != want {
		t.Errorf("sent %q, want %q", sent, want)
	}
	if len(c.input) != 0 || c.intercepting {
		t.Error("a sent box is empty and no longer asked about")
	}
}

func TestEnterSavesToo(t *testing.T) {
	v := &askPlugin{}
	m, c := boxWithKey(t, v)
	land(t, m, m.sendPane(c, false))
	land(t, m, m.confirmKey("enter"))
	if v.saved("OPENAI_API_KEY") != pastedKey || !strings.Contains(sentText(t, c), "{{secret:OPENAI_API_KEY}}") {
		t.Fatalf("enter should save and send: %v", v.stored)
	}
}

func TestNoAndEscSendUnchanged(t *testing.T) {
	for _, key := range []string{"n", "esc"} {
		v := &askPlugin{}
		m, c := boxWithKey(t, v)
		land(t, m, m.sendPane(c, false))
		land(t, m, m.confirmKey(key))
		if len(v.stored) != 0 {
			t.Errorf("%s saved %v", key, v.stored)
		}
		if got := sentText(t, c); got != "deploy with "+pastedKey+" please" {
			t.Errorf("%s sent %q", key, got)
		}
	}
}

func TestCtrlCCancelsTheSend(t *testing.T) {
	v := &askPlugin{}
	m, c := boxWithKey(t, v)
	land(t, m, m.sendPane(c, false))
	if cmd := m.confirmKey("ctrl+c"); cmd != nil || m.confirm != nil {
		t.Fatal("ctrl+c closes the question and asks nothing more")
	}
	if len(c.sending) != 0 || string(c.input) != "deploy with "+pastedKey+" please" || c.intercepting || len(v.stored) != 0 {
		t.Fatalf("ctrl+c leaves the box: sent %d, box %q", len(c.sending), string(c.input))
	}
}

func TestFailedSaveNeverSendsOnItsOwn(t *testing.T) {
	v := &askPlugin{addErr: errors.New("the master is not running")}
	m, c := boxWithKey(t, v)
	land(t, m, m.sendPane(c, false))
	land(t, m, m.confirmKey("y"))
	if len(c.sending) != 0 {
		t.Fatal("a failed save sent the message")
	}
	if m.confirm == nil || m.confirm.question != "Couldn't store the secret" || !strings.Contains(m.confirm.detail, "the master is not running") {
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
	v := &askPlugin{}
	m, c := boxWithKey(t, v)
	gh := "ghp_" + strings.Repeat("Zq7Wx3Rt9", 4)
	c.input = []rune(pastedKey + " and " + gh)
	land(t, m, m.sendPane(c, false))
	land(t, m, m.confirmKey("y"))
	if m.confirm == nil || m.confirm.question != "Save as secret GITHUB_TOKEN?" || len(c.sending) != 0 {
		t.Fatalf("want the second question, got %+v", m.confirm)
	}
	if box := string(c.input); strings.Contains(box, pastedKey) || !strings.HasPrefix(box, "{{secret:OPENAI_API_KEY}} and ") {
		t.Errorf("a saved secret leaves the box, so drafts never keep it: %q", box)
	}
	land(t, m, m.confirmKey("y"))
	sent := sentText(t, c)
	if strings.Contains(sent, pastedKey) || strings.Contains(sent, gh) ||
		!strings.HasPrefix(sent, "{{secret:OPENAI_API_KEY}} and {{secret:GITHUB_TOKEN}}") ||
		!strings.Contains(sent, askUsage("OPENAI_API_KEY")+"\n"+askUsage("GITHUB_TOKEN")) {
		t.Errorf("sent %q", sent)
	}
}

func TestOrdinaryMessageIsNotAsked(t *testing.T) {
	v := &askPlugin{}
	m, c := boxWithKey(t, v)
	c.input = []rune("set OPENAI_API_KEY=sk-1234 or <your-key> and commit 51d07b547d0a8f3e2c1b9d4a6e7f8091a2b3c4d5")
	land(t, m, m.sendPane(c, false))
	if m.confirm != nil || len(c.sending) != 1 {
		t.Fatalf("an ordinary message goes straight out: confirm %+v", m.confirm)
	}
}

func TestPromptAsksBeforeStarting(t *testing.T) {
	v := &askPlugin{}
	m, _ := infoModel(t)
	m.hooks = askHooks(t, v)
	m.input = []rune("use " + pastedKey)
	land(t, m, m.submit())
	if m.confirm == nil || m.confirm.question != "Save as secret OPENAI_API_KEY?" {
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
		t.Fatalf("saved %v, Prompt %q", v.stored, string(m.input))
	}
}

// A secret inside a long paste is saved the same: the chip stays a chip,
// and its text goes out with the reference.
func TestSecretInAPaste(t *testing.T) {
	v := &askPlugin{}
	m, c := infoModel(t)
	m.hooks = askHooks(t, v)
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
		if strings.Contains(p, pastedKey) || !strings.Contains(p, "{{secret:OPENAI_API_KEY}}") {
			t.Fatalf("paste %q", p)
		}
	}
	msg.was, msg.r = string(c.input), plugin.InterceptResult{Action: "allow"}
	m.onIntercepted(msg)
	sent := sentText(t, c)
	if strings.Contains(sent, pastedKey) || !strings.Contains(sent, "OPENAI_API_KEY={{secret:OPENAI_API_KEY}}") {
		t.Errorf("sent %q", sent)
	}
}
