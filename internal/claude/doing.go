package claude

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"
)

// Doing says in words what a tool call is doing, for the one line a list row
// has: "running pnpm test", "reading view.go", "searching for PrettyModel".
// A command's own description wins when Claude gave one.
func Doing(tool string, input json.RawMessage) string {
	var in map[string]any
	_ = json.Unmarshal(input, &in)
	str := func(k string) string {
		s, _ := in[k].(string)
		return oneLine(s)
	}
	base := func(p string) string {
		if p == "" {
			return ""
		}
		return filepath.Base(p)
	}
	with := func(verb, what string) string {
		if what == "" {
			return verb
		}
		return verb + " " + what
	}
	switch tool {
	case "Bash":
		if d := str("description"); d != "" {
			return lowerFirst(d)
		}
		return shell(str("command"))
	case "BashOutput", "TaskOutput":
		return "checking a background task"
	case "KillShell", "TaskStop":
		return "stopping a background task"
	case "Read":
		return with("reading", base(str("file_path")))
	case "Edit", "MultiEdit":
		return with("editing", base(str("file_path")))
	case "Write":
		return with("writing", base(str("file_path")))
	case "NotebookEdit":
		return with("editing", base(str("notebook_path")))
	case "Grep":
		return with("searching for", str("pattern"))
	case "Glob":
		return with("finding", str("pattern"))
	case "WebFetch":
		if u, err := url.Parse(str("url")); err == nil && u.Host != "" {
			return "reading " + strings.TrimPrefix(u.Host, "www.")
		}
		return "reading a web page"
	case "WebSearch":
		return with("searching the web for", str("query"))
	case "Task", "Agent":
		return with("subagent:", lowerFirst(str("description")))
	case "TodoWrite", "TaskCreate", "TaskUpdate":
		return "updating its todo list"
	case "AskUserQuestion":
		if qs, ok := in["questions"].([]any); ok && len(qs) > 0 {
			if q, ok := qs[0].(map[string]any); ok {
				if t, ok := q["question"].(string); ok {
					return oneLine(t)
				}
			}
		}
		return "asking you"
	case "ExitPlanMode":
		return "has a plan for you"
	case "Skill":
		return with("using", str("skill"))
	case "ToolSearch":
		return "looking up tools"
	case "ScheduleWakeup", "Monitor":
		return "waiting"
	}
	if server, name, ok := strings.Cut(strings.TrimPrefix(tool, "mcp__"), "__"); ok && strings.HasPrefix(tool, "mcp__") {
		return strings.ReplaceAll(name, "_", " ") + " · " + strings.TrimPrefix(server, "claude_ai_")
	}
	for _, k := range []string{"description", "command", "file_path", "pattern", "url", "query", "prompt"} {
		if v := str(k); v != "" {
			return tool + " " + v
		}
	}
	return tool
}

// shell says what a shell command does: "searching for X" for grep and rg,
// else "running" and the program it runs with its first argument, past any
// cd, env or variables in front of it: "running pnpm test".
func shell(cmd string) string {
	for _, part := range splitAny(cmd, "&&", ";", "||") {
		f := strings.Fields(part)
		for len(f) > 0 && (strings.Contains(f[0], "=") || f[0] == "env" || f[0] == "sudo" || f[0] == "time") {
			f = f[1:]
		}
		if len(f) == 0 || f[0] == "cd" || f[0] == "export" {
			continue
		}
		prog := filepath.Base(f[0])
		if prog == "grep" || prog == "rg" {
			if pat := pattern(part); pat != "" {
				return "searching for " + pat
			}
			return "searching the code"
		}
		if len(f) > 1 && !strings.HasPrefix(f[1], "-") && !strings.ContainsAny(f[1], `"'/$|<>`) {
			return "running " + prog + " " + f[1]
		}
		return "running " + prog
	}
	return "running a command"
}

// pattern is grep's first argument that isn't a flag, quotes and all.
func pattern(cmd string) string {
	rest := strings.TrimSpace(cmd)
	_, rest, _ = strings.Cut(rest, " ") // the program
	for rest = strings.TrimSpace(rest); rest != ""; rest = strings.TrimSpace(rest) {
		if q := rest[0]; q == '"' || q == '\'' {
			end := strings.IndexByte(rest[1:], q)
			if end < 0 {
				return ""
			}
			return rest[1 : 1+end]
		}
		word, after, _ := strings.Cut(rest, " ")
		if !strings.HasPrefix(word, "-") {
			return word
		}
		if word == "-e" || word == "-A" || word == "-B" || word == "-C" || word == "-m" || word == "-g" || word == "-t" {
			_, after, _ = strings.Cut(strings.TrimSpace(after), " ") // its value
			if word == "-e" {
				return pattern("grep " + strings.TrimSpace(strings.TrimPrefix(rest, "-e")))
			}
		}
		rest = after
	}
	return ""
}

func splitAny(s string, seps ...string) []string {
	parts := []string{s}
	for _, sep := range seps {
		var next []string
		for _, p := range parts {
			next = append(next, strings.Split(p, sep)...)
		}
		parts = next
	}
	return parts
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if len(r) > 1 && r[1] >= 'A' && r[1] <= 'Z' {
		return s // an acronym: "PR", "CI"
	}
	if r[0] >= 'A' && r[0] <= 'Z' {
		r[0] += 'a' - 'A'
	}
	return string(r)
}
