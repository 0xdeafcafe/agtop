package plugind

import (
	"context"
	"encoding/json/jsontext"
	"testing"

	"github.com/0xdeafcafe/rush/internal/jsonx"
	"github.com/0xdeafcafe/rush/internal/plugin"
)

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
