package convo

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/tool"
)

// Spawn is another agent a shell command ran: claude -p, codex exec,
// copilot -p. The session it wrote is found and followed by whoever draws
// the conversation, and handed to SetChild.
type Spawn struct {
	Kind   agent.Kind
	Name   string // what the agent is called: Codex, Claude Code
	Prompt string // what it was asked; empty when it came from a file or a pipe
	From   string // the file its prompt was fed from, when it was
	Model  string
	Dir    string // where it ran, when the command went somewhere first
	// Child is the run a harness's own subagent call names, when its
	// harness keeps it apart (agent.ChildFinder); Kind is then the session's.
	Child string
}

// spawnNot are the first words that make an agent's program do something
// other than work: set up, sign in, list, report.
var spawnNot = map[string]bool{
	"mcp": true, "config": true, "doctor": true, "update": true, "upgrade": true, "install": true, "login": true,
	"logout": true, "auth": true, "plugin": true, "plugins": true, "setup-token": true, "migrate-installer": true,
	"completion": true, "features": true, "apply": true, "app-server": true, "mcp-server": true, "sandbox": true,
	"debug": true, "cloud": true, "models": true, "version": true, "help": true, "agents": true, "acp": true,
	"--version": true, "-v": true, "-V": true, "--help": true, "-h": true, "--acp": true,
}

// spawnSub are the first words that start a run and name nothing else:
// codex exec, opencode run.
var spawnSub = map[string]bool{"exec": true, "e": true, "run": true}

// spawnValued are the flags that take a value, which isn't the prompt.
var spawnValued = map[string]bool{
	"--model": true, "-m": true, "--output-format": true, "--input-format": true, "--append-system-prompt": true,
	"--system-prompt": true, "--allowedTools": true, "--allowed-tools": true, "--disallowedTools": true,
	"--disallowed-tools": true, "--permission-mode": true, "--add-dir": true, "--max-turns": true, "--resume": true,
	"-r": true, "--session-id": true, "--settings": true, "--mcp-config": true, "--agents": true, "--agent": true,
	"--fallback-model": true, "--effort": true, "-C": true, "--cd": true, "-c": true, "--config": true, "-s": true,
	"--sandbox": true, "-i": true, "--image": true, "--profile": true, "-a": true, "--ask-for-approval": true,
	"--output-last-message": true, "-o": true, "--color": true, "--output-schema": true, "--log-level": true,
	"--allow-tool": true, "--deny-tool": true, "--betas": true, "--setting-sources": true, "--plugin-dir": true,
	"--json-schema": true, "--max-budget-usd": true, "--permission-prompt-tool": true, "--permission-prompts": true,
	"--name": true, "-n": true, "--reasoning-effort": true, "--agent-file": true,
}

// spawnPrint are the flags that run the agent once and print: the prompt
// follows them for most agents (claude's -p takes none, so its prompt is
// the next word either way).
var spawnPrint = map[string]bool{"-p": true, "--print": true, "--prompt": true}

// SpawnOf is the agent cmd runs, when it runs one: a registered agent's
// program, asked to work rather than to set up or report.
func SpawnOf(cmd string) (Spawn, bool) {
	segs := segments(strings.ReplaceAll(strings.TrimSpace(cmd), "\\\n", " "))
	dir := ""
	for _, s := range segs {
		c := shellCmd(s.text)
		if len(c.words) >= 2 && c.words[0] == "cd" {
			dir = unquoteArg(c.words[1])
		}
		if sp, ok := spawnIn(c, s.body, ""); ok {
			return withDir(sp, dir), true
		}
		// Fed a prompt through a pipe: echo "…" | claude -p.
		for _, f := range s.filters {
			if sp, ok := spawnIn(shellCmd(f), nil, piped(c.words)); ok {
				if sp.Prompt == "" && sp.From == "" && len(c.words) >= 2 && filepath.Base(c.words[0]) == "cat" {
					sp.From = unquoteArg(c.words[1])
				}
				return withDir(sp, dir), true
			}
		}
	}
	return Spawn{}, false
}

// command is one simple command as the shell reads it: its words (quotes
// kept, a word running on across them), and what it's fed on stdin.
type command struct {
	words []string
	stdin string // a file fed with <
	tag   string // a heredoc fed with <<
}

// shellCmd reads the first simple command of s: up to an unquoted ;, &,
// | or newline, its redirects taken out.
func shellCmd(s string) command {
	var c command
	var cur strings.Builder
	in := false // a word has begun
	redir := "" // the redirect whose target comes next
	flush := func() {
		if !in {
			return
		}
		w := cur.String()
		cur.Reset()
		in = false
		switch redir {
		case "<":
			c.stdin = unquoteArg(w)
		case "<<":
			c.tag = strings.Trim(w, `'"`)
		case "":
			c.words = append(c.words, w)
		}
		redir = ""
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == '\\' && i+1 < len(s):
			cur.WriteByte(ch)
			cur.WriteByte(s[i+1])
			i++
			in = true
		case ch == '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				j = len(s) - i - 1
			}
			cur.WriteString(s[i:min(len(s), i+j+2)])
			i += j + 1
			in = true
		case ch == '"':
			j := closeQuote(s, i)
			cur.WriteString(s[i:j])
			i = j - 1
			in = true
		case ch == '$' && i+1 < len(s) && s[i+1] == '(':
			j := closeParen(s, i+1)
			cur.WriteString(s[i:j])
			i = j - 1
			in = true
		case ch == ' ' || ch == '\t':
			flush()
		case ch == ';' || ch == '|' || ch == '\n' || ch == '&' && !(i+1 < len(s) && s[i+1] == '>'):
			flush()
			return c
		case ch == '<' || ch == '>' || ch == '&':
			flush()
			op := string(ch)
			for i+1 < len(s) && strings.IndexByte("<>&", s[i+1]) >= 0 {
				i++
				op += string(s[i])
			}
			// 2>&1 and >&2 are whole in themselves.
			if strings.HasSuffix(op, "&") && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9' {
				i++
				redir = ""
				continue
			}
			redir = op
			if op != "<" && op != "<<" && op != "<<-" {
				redir = ">"
			} else if op == "<<-" {
				redir = "<<"
			}
			// What's written to is no word of the command.
			in = false
		case ch >= '0' && ch <= '9' && !in && i+1 < len(s) && s[i+1] == '>':
			// the 2 of 2>
		default:
			cur.WriteByte(ch)
			in = true
		}
	}
	flush()
	return c
}

// closeQuote is just past the " that closes the one at i, past any $(…)
// inside it and its own quotes.
func closeQuote(s string, i int) int {
	for j := i + 1; j < len(s); j++ {
		switch {
		case s[j] == '\\':
			j++
		case s[j] == '$' && j+1 < len(s) && s[j+1] == '(':
			j = closeParen(s, j+1) - 1
		case s[j] == '"':
			return j + 1
		}
	}
	return len(s)
}

// closeParen is just past the ) that closes the ( at i.
func closeParen(s string, i int) int {
	depth := 0
	for j := i; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case '\'':
			if k := strings.IndexByte(s[j+1:], '\''); k >= 0 {
				j += k + 1
			}
		case '"':
			j = closeQuote(s, j) - 1
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 {
				return j + 1
			}
		}
	}
	return len(s)
}

func withDir(sp Spawn, dir string) Spawn {
	if sp.Dir == "" {
		sp.Dir = dir
	}
	return sp
}

// piped is what echo or printf writes into a pipe: a prompt given that way.
func piped(words []string) string {
	if len(words) >= 2 && (words[0] == "echo" || words[0] == "printf") {
		for _, w := range words[1:] {
			if !strings.HasPrefix(w, "-") {
				return unquoteArg(w)
			}
		}
	}
	return ""
}

// catRe is a prompt read from a file as it's given: "$(cat prompt.md)".
var catRe = regexp.MustCompile(`^"?\$\((?:cat|<)\s+("[^"]+"|'[^']+'|[^\s)]+)\s*\)"?$`)

// spawnIn reads one command as an agent run: its program (past any runner
// such as timeout or npx), and then its prompt and model. stdin is what
// a pipe fed it, and body the lines after its own, for a heredoc.
func spawnIn(c command, body []string, stdin string) (Spawn, bool) {
	words := c.words
	for len(words) > 0 && assignRe.MatchString(words[0]) {
		words = words[1:]
	}
	for len(words) > 1 {
		switch w := filepath.Base(words[0]); w {
		case "timeout", "gtimeout":
			words = words[1:]
			for len(words) > 1 && strings.HasPrefix(words[0], "-") {
				words = words[1:]
			}
			if len(words) > 1 {
				words = words[1:] // the duration
			}
			continue
		case "env", "nohup", "time", "exec", "command", "caffeinate", "npx", "bunx", "pnpx":
			words = words[1:]
			for len(words) > 1 && (strings.HasPrefix(words[0], "-") || assignRe.MatchString(words[0])) {
				words = words[1:]
			}
			continue
		}
		break
	}
	if len(words) == 0 {
		return Spawn{}, false
	}
	if len(words) >= 3 && filepath.Base(words[0]) == "rush" && words[1] == "session" && words[2] == "start" {
		return rushStart(words[3:], c, body, stdin)
	}
	k, ok := agent.ProgramKind(unquoteArg(words[0]))
	if !ok {
		return Spawn{}, false
	}
	sp := Spawn{Kind: k, Name: string(k)}
	if a, ok := agent.Get(k); ok {
		sp.Name = a.Name()
	}
	args := words[1:]
	print, sub, fromStdin, streamed := false, false, false, false
	var loose []string // bare words after a flag rush doesn't know, which may be its value
	for i := 0; i < len(args); i++ {
		a := args[i]
		// The subcommand is the first bare word, past any global flags:
		// codex --search exec.
		first := !sub && !print && sp.Prompt == "" && sp.From == "" && len(loose) == 0
		switch {
		case spawnNot[a] && sp.Prompt == "" && !sub:
			return Spawn{}, false
		case spawnSub[a] && first:
			sub = true
		case spawnPrint[a]:
			print = true
		case a == "-":
			fromStdin = true
		case strings.HasPrefix(a, "-"):
			name, val, eq := strings.Cut(a, "=")
			next := ""
			if i+1 < len(args) {
				next = args[i+1]
			}
			if eq {
				next = val
			}
			switch name {
			case "--model", "-m":
				sp.Model = unquoteArg(next)
			case "-C", "--cd":
				sp.Dir = unquoteArg(next)
			case "--input-format":
				streamed = unquoteArg(next) == "stream-json"
			}
			switch {
			case eq:
			case spawnValued[name]:
				i++
			case strings.HasPrefix(name, "--") && next != "" && !strings.HasPrefix(next, "-") && !quoted(next) && (!first || !spawnSub[next]):
				loose = append(loose, next)
				i++
			}
		case sp.Prompt == "" && sp.From == "":
			if m := catRe.FindStringSubmatch(a); m != nil {
				sp.From = unquoteArg(m[1])
			} else {
				sp.Prompt = promptOf(a, body)
			}
		}
	}
	// A bare word after a flag rush doesn't know is the prompt only when
	// nothing else is.
	if sp.Prompt == "" && sp.From == "" && len(loose) > 0 && !fromStdin && c.stdin == "" && c.tag == "" && stdin == "" {
		sp.Prompt = unquoteArg(loose[len(loose)-1])
	}
	if sp.Prompt == "" && sp.From == "" {
		switch {
		case stdin != "":
			sp.Prompt = stdin
		case c.tag != "":
			sp.Prompt = strings.TrimSpace(strings.Join(heredoc(body, c.tag), "\n"))
		case c.stdin != "" && c.stdin != "/dev/null":
			sp.From = c.stdin
		}
	}
	// Messages as JSON aren't a prompt to show.
	if streamed {
		sp.Prompt = ""
	}
	// A bare program with nothing asked of it opens its own screen, and
	// codex needs exec to run without one.
	if sp.Kind == "codex" && !sub {
		return Spawn{}, false
	}
	if !print && !sub && sp.Prompt == "" && sp.From == "" {
		return Spawn{}, false
	}
	return sp, true
}

// rushStart reads rush session start's args as the agent it starts: a
// session of its own, which rush lists as its starter's subagent.
func rushStart(args []string, c command, body []string, stdin string) (Spawn, bool) {
	sp := Spawn{Kind: agent.LegacyKind}
	if slices.Contains(args, "--resume") {
		return Spawn{}, false // one it had already
	}
	for i := 0; i+1 < len(args); i++ {
		name, val, eq := strings.Cut(args[i], "=")
		if !eq {
			val = args[i+1]
		}
		switch v := unquoteArg(val); name {
		case "--agent":
			sp.Kind = agent.Kind(v)
		case "--model":
			sp.Model = v
		case "--cwd":
			sp.Dir = v
		case "--prompt-file":
			sp.From = v
		}
	}
	if sp.From == "-" {
		sp.From = ""
		switch {
		case stdin != "":
			sp.Prompt = stdin
		case c.tag != "":
			sp.Prompt = strings.TrimSpace(strings.Join(heredoc(body, c.tag), "\n"))
		case c.stdin != "":
			sp.From = c.stdin
		}
	}
	sp.Name = string(sp.Kind)
	if a, ok := agent.Get(sp.Kind); ok {
		sp.Name = a.Name()
	}
	return sp, true
}

func quoted(w string) bool { return strings.HasPrefix(w, `"`) || strings.HasPrefix(w, "'") }

// promptOf is a prompt as written: quoted, or fed in by $(cat <<'EOF' …).
func promptOf(w string, body []string) string {
	if m := heredocArg.FindStringSubmatch(w); m != nil {
		// Quoted, the heredoc is part of the word; bare, it's the lines
		// that follow the command's.
		if v, ok := argValue(w); ok && strings.Contains(w, "\n") {
			return strings.TrimSpace(v)
		}
		return strings.TrimSpace(strings.Join(heredoc(body, m[1]), "\n"))
	}
	return unquoteArg(w)
}

// heredoc is a heredoc's lines, up to its tag.
func heredoc(body []string, tag string) []string {
	for i, l := range body {
		if strings.TrimSpace(l) == tag || strings.HasPrefix(strings.TrimSpace(l), tag+")") {
			return body[:i]
		}
	}
	return body
}

// unquoteArg is a word as the program gets it, escapes and all.
func unquoteArg(w string) string {
	if len(w) >= 2 && (w[0] == '"' || w[0] == '\'') {
		if v, ok := argValue(w); ok {
			return v
		}
	}
	return w
}

// Spawn is the agent the step's command ran, when it ran one, or the
// subagent it started when that keeps a session of its own.
func (st *Step) Spawn() (Spawn, bool) {
	if st.kind() == tool.Subagent {
		in := st.in()
		if in.Child == "" {
			return Spawn{}, false
		}
		return Spawn{Name: agentName(st), Prompt: firstNonEmpty(in.Prompt, in.Description), Child: in.Child}, true
	}
	if st.kind() != tool.Shell {
		return Spawn{}, false
	}
	if st.spawnAt != len(st.Input)+1 {
		st.spawnAt = len(st.Input) + 1
		st.spawn = nil
		if sp, ok := SpawnOf(st.in().Command); ok {
			st.spawn = &sp
		}
	}
	if st.spawn == nil {
		return Spawn{}, false
	}
	return *st.spawn, true
}

// Child is the session a spawned agent wrote, once SetChild found it.
func (st *Step) Child() *Session { return st.child }

// Spawns are the steps that started an agent whose session is its own, in
// order: from the shell, or as a harness's subagent kept apart.
func (s *Session) Spawns() []*Step {
	var out []*Step
	for _, t := range s.Turns {
		for _, it := range t.Items {
			if it.Kind == KStep && it.Step != nil {
				if _, ok := it.Step.Spawn(); ok {
					out = append(out, it.Step)
				}
			}
		}
	}
	return out
}

// SetChild shows child as what st's spawned agent did: its steps are
// drawn under st's row like a subagent's. Call it again when child grows.
func (s *Session) SetChild(st *Step, child *Session) {
	var kids []*Step
	for _, t := range child.Turns {
		for _, it := range t.Items {
			if it.Kind == KStep && it.Step != nil && !hidden(it.Step) {
				kids = append(kids, it.Step)
			}
		}
	}
	st.child, st.Children = child, kids
	s.touchStep(st)
}

// spawnShown is how many of a running spawned agent's steps show under
// its row.
const spawnShown = 4

// spawnLabel is a spawned agent's row, as any subagent's: ⇉, the agent's
// name, then what it was asked (or what the command says it's for).
func spawnLabel(sp Spawn, desc string, lbl func(string) string) string {
	what := oneLine(sp.Prompt)
	switch {
	case what == "" && sp.From != "":
		what = "prompt from " + filepath.Base(sp.From)
	case what == "":
		what = desc
	}
	return glyphColor("⇉") + " " + lbl(sp.Name) + "  " + faint(what)
}

// reply is what a subagent said back, drawn as its opened row's body: the
// last answer of its own session once that's found, else what its call
// returned (a spawned agent's stdout, or its stderr when that's all).
func reply(st *Step) string {
	if st.child != nil {
		if a := st.child.LastAnswer(1); a != "" {
			return a
		}
	}
	o := st.out()
	return firstNonEmpty(o.Stdout, o.Stderr, st.Output)
}
