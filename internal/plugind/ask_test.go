package plugind

import (
	"context"
	"encoding/json/jsontext"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xdeafcafe/rush/internal/bundled/kanbanvault"
	"github.com/0xdeafcafe/rush/internal/jsonx"
	"github.com/0xdeafcafe/rush/internal/plugin"
)

// addRun is addPlugin for a plugin whose main is run, as a bundled one's.
func addRun(t *testing.T, b *broker, m *plugin.Manifest, run func(rw net.Conn) error) {
	t.Helper()
	mine, theirs := net.Pipe()
	go func() { _ = run(theirs) }()
	r := newRunner(b, m.Name, "")
	bc := plugin.NewConn(mine, r.fromPlugin)
	r.mu.Lock()
	r.p, r.conn, r.out, r.state = plugin.Plugin{Manifest: *m, Bundled: true}, bc, newOutbox(bc, pluginQueue), "running"
	r.mu.Unlock()
	b.mu.Lock()
	b.plugins[m.Name] = r
	b.mu.Unlock()
	t.Cleanup(func() { bc.Close(); theirs.Close() })
}

// A plugin may ask about a message: the chain stops at its question, with
// what the plugins before it changed, and the key chosen goes back to it.
// What it then says changes the message, and the plugins after it are
// asked as usual.
func TestUIInterceptAsk(t *testing.T) {
	b := testBroker(t)
	intercept := []string{plugin.UIInput, plugin.UIIntercept}
	var answered plugin.InterceptAnswer
	addPlugin(t, b, &plugin.Manifest{Name: "a", UI: intercept}, func(_ context.Context, method string, params jsontext.Value) (any, error) {
		var in plugin.Intercept
		_ = jsonx.Unmarshal(params, &in)
		return plugin.InterceptResult{Action: "rewrite", Replace: []plugin.Replacement{{Old: "hi", New: "hello"}}}, nil
	})
	addPlugin(t, b, &plugin.Manifest{Name: "b", UI: intercept}, func(_ context.Context, method string, params jsontext.Value) (any, error) {
		switch method {
		case "ui.intercept":
			return plugin.InterceptResult{Action: "ask", ID: "q1", Question: "Really\x1b?", Detail: "it says hello",
				Choices: []plugin.AskChoice{{Key: "y", Label: "yes", Enter: true}, {Key: "n", Label: "no", Esc: true}}}, nil
		case "ui.intercept.answer":
			_ = jsonx.Unmarshal(params, &answered)
			if answered.Key == "n" {
				return plugin.InterceptResult{Action: "block", Reason: "you said no"}, nil
			}
			return plugin.InterceptResult{Action: "rewrite", Append: "\n\nsigned"}, nil
		}
		return map[string]any{}, nil
	})
	addPlugin(t, b, &plugin.Manifest{Name: "c", UI: intercept}, func(_ context.Context, method string, params jsontext.Value) (any, error) {
		var in plugin.Intercept
		_ = jsonx.Unmarshal(params, &in)
		return plugin.InterceptResult{Action: "rewrite", Text: in.Text + "!"}, nil
	})
	u := attachUI(t, b, "main")

	var ask plugin.InterceptResult
	if err := u.call("ui.intercept", plugin.Intercept{Hook: "before-send", UI: "main", Box: "s1", Text: "hi there"}, &ask); err != nil {
		t.Fatal(err)
	}
	if ask.Action != "ask" || ask.Plugin != "b" || ask.ID != "q1" || ask.Question != "Really?" || ask.Text != "hello there" ||
		len(ask.Replace) != 1 || len(ask.Choices) != 2 {
		t.Fatalf("ask = %+v", ask)
	}
	var res plugin.InterceptResult
	if err := u.call("ui.intercept.answer", plugin.InterceptAnswer{Intercept: plugin.Intercept{Hook: "before-send", UI: "main", Box: "s1", Text: ask.Text},
		Plugin: "b", ID: ask.ID, Key: "y"}, &res); err != nil {
		t.Fatal(err)
	}
	if answered.ID != "q1" || answered.Key != "y" || answered.Text != "hello there" || answered.Box != "s1" {
		t.Fatalf("b was handed %+v", answered)
	}
	if res.Action != "rewrite" || res.Text != "hello there\n\nsigned!" || res.Plugin != "b, c" {
		t.Fatalf("after the answer = %+v", res)
	}
	if err := u.call("ui.intercept.answer", plugin.InterceptAnswer{Intercept: plugin.Intercept{Text: ask.Text}, Plugin: "b", ID: "q1", Key: "n"}, &res); err != nil {
		t.Fatal(err)
	}
	if res.Action != "block" || res.Plugin != "b" || res.Reason != "you said no" {
		t.Fatalf("a no = %+v", res)
	}
	// An answer for a plugin that isn't there holds the message back.
	if err := u.call("ui.intercept.answer", plugin.InterceptAnswer{Intercept: plugin.Intercept{Text: "x"}, Plugin: "gone", Key: "y"}, &res); err != nil {
		t.Fatal(err)
	}
	if res.Action != "block" {
		t.Fatalf("an answer nobody took = %+v", res)
	}
}

// An ask rush can't show is taken as allow.
func TestUIInterceptBadAsk(t *testing.T) {
	b := testBroker(t)
	addPlugin(t, b, &plugin.Manifest{Name: "a", UI: []string{plugin.UIInput, plugin.UIIntercept}}, func(context.Context, string, jsontext.Value) (any, error) {
		return plugin.InterceptResult{Action: "ask", Question: "?", Choices: []plugin.AskChoice{{Key: "Yes", Label: "y"}}}, nil
	})
	u := attachUI(t, b, "main")
	var res plugin.InterceptResult
	if err := u.call("ui.intercept", plugin.Intercept{Text: "hi"}, &res); err != nil || res.Action != "allow" {
		t.Fatalf("got %+v, %v", res, err)
	}
}

// fakeKV is a kv that keeps its secrets in dir: ls --json lists them, add
// NAME saves stdin under NAME.
func fakeKV(t *testing.T, existing ...string) (bin, dir string) {
	t.Helper()
	dir = t.TempDir()
	for _, n := range existing {
		_ = os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600)
	}
	bin = filepath.Join(t.TempDir(), "kv")
	script := `#!/bin/sh
d='` + dir + `'
case "$1" in
ls) printf '['; sep=''; for f in "$d"/*; do [ -e "$f" ] || continue; printf '%s{"name":"%s"}' "$sep" "$(basename "$f")"; sep=','; done; printf ']\n' ;;
add) cat > "$d/$2" ;;
*) echo "kv: no such command" >&2; exit 2 ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, dir
}

// The kanban-vault plugin through the broker, as a window uses it: a
// pasted key is asked about, y saves it with kv (run by exec) and the
// message goes with its reference and the line on using it.
func TestKanbanVaultThroughTheBroker(t *testing.T) {
	b := testBroker(t)
	kv, dir := fakeKV(t, "OPENAI_API_KEY")
	m := kanbanvault.Manifest
	m.Exec = map[string][]string{"kv": {kv}}
	addRun(t, b, &m, func(rw net.Conn) error { return kanbanvault.Run(rw) })
	u := attachUI(t, b, "main")

	key := "sk-proj-" + strings.Repeat("Ab3dEf7gHi2jKlMn", 3)
	text := "deploy with " + key + " please"
	var ask plugin.InterceptResult
	if err := u.call("ui.intercept", plugin.Intercept{Hook: "before-send", UI: "main", Box: "s1", Text: text}, &ask); err != nil {
		t.Fatal(err)
	}
	if ask.Action != "ask" || ask.Plugin != kanbanvault.Name || !strings.HasPrefix(ask.Question, "Save as vault secret OPENAI_API_KEY") ||
		strings.Contains(ask.Detail, key) {
		t.Fatalf("ask = %+v", ask)
	}
	// Asked before the vault's names were listed, it offers the name
	// again once a save finds it taken.
	res := ask
	for range 2 {
		q := res
		if err := u.call("ui.intercept.answer", plugin.InterceptAnswer{Intercept: plugin.Intercept{Hook: "before-send", UI: "main", Box: "s1", Text: q.Text},
			Plugin: q.Plugin, ID: q.ID, Key: "y"}, &res); err != nil {
			t.Fatal(err)
		}
		if res.Action != "ask" {
			break
		}
		if res.Question != "Save as vault secret OPENAI_API_KEY_2?" {
			t.Fatalf("offered again = %+v", res)
		}
	}
	if res.Action != "rewrite" || strings.Contains(res.Text, key) || !strings.Contains(res.Text, "deploy with {{vault:OPENAI_API_KEY_2}} please") ||
		!strings.Contains(res.Text, "kv run OPENAI_API_KEY_2") {
		t.Fatalf("after y = %+v", res)
	}
	if saved, _ := os.ReadFile(filepath.Join(dir, "OPENAI_API_KEY_2")); string(saved) != key {
		t.Fatalf("kv was given %q", saved)
	}
}
