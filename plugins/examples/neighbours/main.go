// Command neighbours is the smallest useful agtop plugin: one tool that
// tells Claude which other agents are working in the same repository, and
// on which branch, so it doesn't trip over them. It needs only the list
// capability, and shows the whole shape of a plugin: answer initialize,
// offer tools, answer tool calls, call agtop back.
//
//	mkdir -p ~/.config/agtop/plugins/neighbours
//	go build -o ~/.config/agtop/plugins/neighbours/neighbours ./plugins/examples/neighbours
//	cp plugins/examples/neighbours/plugin.json ~/.config/agtop/plugins/neighbours/
//	agtop plugin approve neighbours
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/0xdeafcafe/agtop/internal/plugin"
)

var conn *plugin.Conn

func main() {
	f := os.NewFile(3, "agtop")
	if f == nil {
		fmt.Fprintln(os.Stderr, "run me from agtop: I talk on fd 3")
		os.Exit(2)
	}
	conn = plugin.NewConn(f, handle)
	<-conn.Done()
}

func handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case "initialize":
		return map[string]any{}, nil
	case "tools.list":
		return map[string]any{"tools": []map[string]any{{
			"name":        "neighbours",
			"description": "List the other agents working in this repository right now: their branch, state and what they're doing. Check before touching shared files, switching branches or running migrations.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
		}}}, nil
	case "tools.call":
		// Who's calling comes with the call: agtop's id for the session,
		// and its folder.
		var in struct {
			Session string `json:"session"`
			Cwd     string `json:"cwd"`
		}
		if err := json.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		text, err := neighbours(ctx, in.Session, in.Cwd)
		if err != nil {
			return result(err.Error(), true), nil
		}
		return result(text, false), nil
	}
	return nil, &plugin.Error{Code: plugin.CodeNoMethod, Message: "method not found: " + method}
}

type session struct {
	ID, Name, Cwd, Repo, Branch, State, Detail string
	Worktree                                   bool
}

func neighbours(ctx context.Context, me, cwd string) (string, error) {
	var all []session
	if err := conn.Call(ctx, "sessions.list", nil, &all); err != nil {
		return "", err
	}
	// The main checkout and its worktrees are one repository: compare where
	// they keep their worktrees from, the part before .claude/worktrees.
	repo := ""
	for _, s := range all {
		if s.ID == me {
			repo = home(s.Repo)
		}
	}
	if repo == "" {
		repo = home(cwd)
	}
	var b strings.Builder
	for _, s := range all {
		if s.ID == me || s.State == "stopped" || repo == "" || home(s.Repo) != repo {
			continue
		}
		fmt.Fprintf(&b, "- %s (%s) on %s, %s", s.Name, s.ID, or(s.Branch, "?"), s.State)
		if s.Worktree {
			b.WriteString(" in its own worktree")
		}
		if s.Detail != "" {
			fmt.Fprintf(&b, ": %s", s.Detail)
		}
		b.WriteString("\n")
	}
	if b.Len() == 0 {
		return "No other agents are working in this repository.", nil
	}
	return b.String(), nil
}

// home is the main checkout a checkout belongs to, going by where agtop and
// Claude Code put worktrees.
func home(repo string) string {
	if i := strings.Index(repo, "/.claude/worktrees/"); i >= 0 {
		return repo[:i]
	}
	return repo
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func result(text string, isErr bool) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": isErr}
}
