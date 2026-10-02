package kanbanvault

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/0xdeafcafe/rush/internal/bundled/kanbanvault/secrets"
	"github.com/0xdeafcafe/rush/internal/plugin"
)

type vault struct {
	mu     sync.Mutex
	names  []string
	added  map[string]string
	lsErr  error
	addErr error
}

func (v *vault) Names(context.Context) ([]string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return slices.Clone(v.names), v.lsErr
}

func (v *vault) setNames(names ...string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.names = names
}

func (v *vault) Add(_ context.Context, name, value string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.addErr != nil {
		return v.addErr
	}
	if v.added == nil {
		v.added = map[string]string{}
	}
	v.added[name] = value
	v.names = append(v.names, name)
	return nil
}

var key = "sk-proj-" + strings.Repeat("Ab3dEf7gHi2jKlMn", 3)

func ready(v Vault) *app {
	a := newApp(v)
	close(a.ready)
	return a
}

func TestNothingToAskAbout(t *testing.T) {
	a := ready(&vault{})
	if r := a.intercept(plugin.Intercept{Text: "set OPENAI_API_KEY=sk-1234 and <your-key>"}); r.Action != "allow" {
		t.Fatalf("got %+v", r)
	}
}

// The name offered can be taken by the time you say y: the save offers
// the next free one instead.
func TestTakenNameIsOfferedAgain(t *testing.T) {
	v := &vault{}
	a := ready(v)
	ask := a.intercept(plugin.Intercept{Box: "s1", Text: "OPENAI_API_KEY=" + key})
	if ask.Question != "Save as vault secret OPENAI_API_KEY?" {
		t.Fatalf("ask %+v", ask)
	}
	v.setNames("OPENAI_API_KEY")
	again := a.answer(context.Background(), plugin.Intercept{Box: "s1", Text: "OPENAI_API_KEY=" + key}, ask.ID, "y")
	if again.Action != "ask" || again.Question != "Save as vault secret OPENAI_API_KEY_2?" || len(v.added) != 0 {
		t.Fatalf("again %+v", again)
	}
	done := a.answer(context.Background(), plugin.Intercept{Box: "s1", Text: "OPENAI_API_KEY=" + key}, again.ID, "y")
	if done.Action != "rewrite" || v.added["OPENAI_API_KEY_2"] != key {
		t.Fatalf("done %+v, added %v", done, v.added)
	}
	if got := done.Apply("OPENAI_API_KEY=" + key); got != secrets.Replace("OPENAI_API_KEY="+key, key, "OPENAI_API_KEY_2") {
		t.Fatalf("sent %q", got)
	}
	// The next secret's question knows the name just saved.
	if next := a.intercept(plugin.Intercept{Box: "s1", Text: "OPENAI_API_KEY=" + key + "x"}); next.Question != "Save as vault secret OPENAI_API_KEY_3?" {
		t.Fatalf("next %+v", next)
	}
}

// A secret you let go isn't asked about again in that box until it's sent
// or cleared.
func TestLetGoUntilSent(t *testing.T) {
	a := ready(&vault{})
	in := plugin.Intercept{Box: "s1", Text: "use " + key}
	ask := a.intercept(in)
	if r := a.answer(context.Background(), in, ask.ID, "n"); r.Action != "allow" {
		t.Fatalf("n = %+v", r)
	}
	if r := a.intercept(in); r.Action != "allow" {
		t.Fatalf("asked again: %+v", r)
	}
	if r := a.intercept(plugin.Intercept{Box: "s2", Text: in.Text}); r.Action != "ask" {
		t.Fatal("another box is asked")
	}
	a.event(uiEvent{Kind: plugin.EvInputSent, Session: &plugin.UISession{ID: "s1"}})
	if r := a.intercept(in); r.Action != "ask" {
		t.Fatal("sent, the box is asked about again")
	}
}

func TestMissingVault(t *testing.T) {
	a := ready(&vault{lsErr: errors.New("kv isn't installed: Kanban Code installs it in ~/.local/bin")})
	in := plugin.Intercept{Text: "use " + key}
	ask := a.intercept(in)
	r := a.answer(context.Background(), in, ask.ID, "y")
	if r.Action != "ask" || r.Question != "Couldn't save the secret to the vault" || !strings.Contains(r.Detail, "kv isn't installed") {
		t.Fatalf("got %+v", r)
	}
	if err := plugin.CleanAsk(&r); err != nil {
		t.Fatal(err)
	}
	if back := a.answer(context.Background(), in, r.ID, "n"); back.Action != "block" {
		t.Fatalf("n after a failure = %+v", back)
	}
}

func TestStaleQuestion(t *testing.T) {
	a := ready(&vault{})
	if r := a.answer(context.Background(), plugin.Intercept{Text: key}, "nope", "y"); r.Action != "block" {
		t.Fatalf("got %+v", r)
	}
}

func TestHint(t *testing.T) {
	if h := Hint(key); h != "sk-p… 56 chars" {
		t.Fatal(h)
	}
}

// Swap says secrets.Replace as a replacement and what it appends, a usage
// line under one already there.
func TestSwap(t *testing.T) {
	text := "a " + key
	once := Swap(text, key, "A").Apply(text)
	if once != secrets.Replace(text, key, "A") {
		t.Fatalf("once %q", once)
	}
	other := "ghp_" + strings.Repeat("Zq7Wx3Rt9", 4)
	twice := Swap(once+" "+other, other, "B")
	if !strings.HasPrefix(twice.Append, "\n") || twice.Apply(once+" "+other) != secrets.Replace(once+" "+other, other, "B") {
		t.Fatalf("twice %+v", twice)
	}
}

func TestManifestIsBundleable(t *testing.T) {
	b, ok := plugin.BundleNamed(Name)
	if !ok || !b.Optional || b.Manifest.ExecArgv("kv") == nil {
		t.Fatalf("bundle %+v", b)
	}
}
