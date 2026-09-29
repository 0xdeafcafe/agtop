package main

// open goes to the featured session the way you would: by name.
var open = []string{"ctrl+k", "text=add Apple Pay to checkout", "enter"}

func view(v string) func(*world) { return func(w *world) { w.view(v) } }

// shots are the README's screenshots, each at the screen it shows.
var shots = []shot{
	{name: "agents", w: 180, h: 62, prep: view("list")},
	{name: "session", w: 180, h: 62, prep: view("agent"), keys: open},
	{name: "session-code", w: 180, h: 58, prep: view("split"), keys: append(open, "up", "up", "up", "up", "up", "up", "up", "up", "up", "space")},
	{name: "overview", w: 180, h: 46, keys: append(open, "]")},
	{name: "changes", w: 180, h: 52, keys: append(open, "]", "]", "up", "up", "space")},
	{name: "subagents", w: 180, h: 46, keys: append(open, "]", "]", "]", "]")},
	{name: "accounts", w: 180, h: 40, keys: []string{"ctrl+k", "text=accounts", "enter"}},
	{name: "coding-agents", w: 180, h: 40, keys: []string{"ctrl+k", "text=settings agents", "enter"}},
	{name: "command-bar", w: 180, h: 62, prep: view("list"), keys: []string{"ctrl+k"}},
	{name: "command-bar-search", w: 180, h: 46, prep: view("split"), keys: append(open, "ctrl+k", "text=merchant")},
	{name: "processes", w: 180, h: 46, keys: []string{"ctrl+k", "text=projects", "enter", "pgdown", "pgdown", "pgdown", "pgdown"}},
	{name: "cleanup", w: 180, h: 46, keys: []string{"ctrl+k", "text=projects", "enter"}},
}
