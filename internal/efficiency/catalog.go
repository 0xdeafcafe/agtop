package efficiency

import "strings"

// Kind is what a saver cuts, which says which figures should move when it's
// on.
type Kind int

const (
	KindToolOutput Kind = iota // what tools send back
	KindTerse                  // what Claude writes
	KindContext                // what the context carries: compaction, subagents
	KindRetrieval              // reading less to find things
	KindMeasure                // seeing where tokens go
)

var KindNames = []string{"Cuts tool output", "Cuts what Claude writes", "Keeps context small", "Finds code with less reading", "Measures"}

// Saver is one thing that saves tokens (or shows where they go): a tool,
// a plugin, an MCP server or one of Claude Code's own settings.
type Saver struct {
	ID    string
	Name  string
	About string
	Kind  Kind
	URL   string
	// Claim is what its authors say it saves, and Measured what someone
	// else measured, when anyone has: shown as such, never as agtop's.
	Claim, Measured string
	// Note is a caution shown before installing.
	Note string

	// Install and Remove are ways to do it; the first whose program is
	// found is used. Manual savers have none: their commands are shown to
	// copy.
	Install, Remove []Recipe
	Manual          []string

	// Setting is a saver that is a Claude Code setting.
	Setting *Setting

	Detect Detect
	// Uses match what the scan records: "hook:" a hook's command, "skill:",
	// "cmd:" a slash command, "mcp:" a server, "bash:" a program run.
	// Each is a prefix or, with a leading *, a substring.
	Uses []string
	// Moves are the figures it should move, for the before/after panel.
	Moves []Metric
}

// Recipe is one way to install or remove a saver: commands run in order,
// needing Needs on the PATH. A step whose Skip program is already found is
// left out.
type Recipe struct {
	Needs string
	Steps []Step
}

type Step struct {
	Argv []string
	Skip string // leave the step out when this program is found
}

// Setting is a saver that's a value in settings.json: a top-level key or,
// with Env, a variable in its env block.
type Setting struct {
	Key   string
	Env   bool
	Value any
	// Default is what Claude Code does when it's unset, in words.
	Default string
}

// Detect is how a saver is found: any of these present means installed.
type Detect struct {
	Bins    []string // programs on the PATH
	Hooks   []string // substrings of a hook's command in settings.json
	Plugins []string // plugin IDs (name@marketplace) enabled
	MCP     []string // MCP server names, user-wide or in the plugin
	Files   []string // files under the account's folder
	// Need is what "on" means when it's more than being found: "hook"
	// (the binary alone is half set up), "plugin", "mcp".
	Need string
}

func step(argv ...string) Step { return Step{Argv: argv} }

// Catalog is agtop's curated list. Install commands are the projects' own
// documented ones, checked September 2026.
var Catalog = []Saver{
	{
		ID: "rtk", Name: "rtk", Kind: KindToolOutput, URL: "https://github.com/rtk-ai/rtk",
		About:    "Rust Token Killer: a hook rewrites Claude's shell commands (git, ls, tests, builds) so their output comes back compressed",
		Claim:    "60–90% less shell output",
		Measured: "JetBrains, 86 tasks: cost flat at high effort, +7.6% at low; only Bash output is touched",
		Install: []Recipe{
			{Needs: "brew", Steps: []Step{{Argv: []string{"brew", "install", "rtk"}, Skip: "rtk"}, step("rtk", "init", "-g", "--auto-patch")}},
			{Needs: "cargo", Steps: []Step{{Argv: []string{"cargo", "install", "--git", "https://github.com/rtk-ai/rtk"}, Skip: "rtk"}, step("rtk", "init", "-g", "--auto-patch")}},
		},
		Remove: []Recipe{{Needs: "rtk", Steps: []Step{step("rtk", "init", "-g", "--uninstall")}}},
		Detect: Detect{Bins: []string{"rtk"}, Hooks: []string{"rtk hook", "rtk-rewrite"}, Files: []string{"RTK.md"}, Need: "hook"},
		Uses:   []string{"hook:*rtk hook", "hook:*rtk-rewrite", "bash:rtk"},
		Moves:  []Metric{MetricToolKB, MetricCtx, MetricCost},
	},
	{
		ID: "caveman", Name: "caveman", Kind: KindTerse, URL: "https://github.com/JuliusBrussee/caveman",
		About:    "a plugin that has Claude answer in terse caveman prose; code, commands and errors stay exact",
		Claim:    "~65% fewer output tokens",
		Measured: "JetBrains: −8.5% output tokens, same quality; output is a small share of most bills",
		Install: []Recipe{{Needs: "claude", Steps: []Step{
			step("claude", "plugin", "marketplace", "add", "JuliusBrussee/caveman"),
			step("claude", "plugin", "install", "caveman@caveman"),
		}}},
		Remove: []Recipe{{Needs: "claude", Steps: []Step{step("claude", "plugin", "uninstall", "caveman@caveman")}}},
		Detect: Detect{Plugins: []string{"caveman@caveman"}, Hooks: []string{"caveman-activate"}, Files: []string{"skills/caveman/SKILL.md"}},
		Uses:   []string{"hook:*caveman", "skill:caveman", "cmd:/caveman"},
		Moves:  []Metric{MetricOut, MetricCost},
	},
	{
		ID: "context-mode", Name: "context-mode", Kind: KindToolOutput, URL: "https://github.com/mksglu/context-mode",
		About: "a plugin that runs commands and reads in a sandbox, indexes the output and returns only what's asked for",
		Claim: "up to 98% less tool output",
		Note:  "adds its own tools and hooks to every session",
		Install: []Recipe{{Needs: "claude", Steps: []Step{
			step("claude", "plugin", "marketplace", "add", "mksglu/context-mode"),
			step("claude", "plugin", "install", "context-mode@context-mode"),
		}}},
		Remove: []Recipe{{Needs: "claude", Steps: []Step{step("claude", "plugin", "uninstall", "context-mode@context-mode")}}},
		Detect: Detect{Plugins: []string{"context-mode@context-mode"}, MCP: []string{"context-mode"}},
		Uses:   []string{"mcp:*context-mode", "hook:*context-mode"},
		Moves:  []Metric{MetricToolKB, MetricCtx},
	},
	{
		ID: "serena", Name: "Serena", Kind: KindRetrieval, URL: "https://github.com/oraios/serena",
		About: "an MCP server that reads and edits code by symbol, through language servers, instead of whole files",
		Claim: "fewer, more targeted reads",
		Note:  "its tool definitions add to what every session starts with",
		Install: []Recipe{{Needs: "uv", Steps: []Step{
			{Argv: []string{"uv", "tool", "install", "-p", "3.13", "serena-agent"}, Skip: "serena"},
			step("claude", "mcp", "add", "--scope", "user", "serena", "--", "serena", "start-mcp-server", "--context", "claude-code", "--project-from-cwd"),
		}}},
		Remove: []Recipe{{Needs: "claude", Steps: []Step{step("claude", "mcp", "remove", "--scope", "user", "serena")}}},
		Detect: Detect{MCP: []string{"serena"}, Bins: []string{"serena"}, Need: "mcp"},
		Uses:   []string{"mcp:serena"},
		Moves:  []Metric{MetricToolKB, MetricStart},
	},
	{
		ID: "graphify", Name: "Graphify", Kind: KindRetrieval, URL: "https://github.com/Graphify-Labs/graphify",
		About: "builds a knowledge graph of the repo and has Claude query it before searching files",
		Claim: "6–70× fewer tokens per question on its benchmarks",
		Install: []Recipe{{Needs: "uv", Steps: []Step{
			{Argv: []string{"uv", "tool", "install", "graphifyy"}, Skip: "graphify"},
			step("graphify", "install"),
		}}},
		Remove: []Recipe{{Needs: "graphify", Steps: []Step{step("graphify", "uninstall")}}},
		Detect: Detect{Bins: []string{"graphify"}, Hooks: []string{"graphify"}, Files: []string{"skills/graphify/SKILL.md"}, Need: "hook"},
		Uses:   []string{"skill:graphify", "cmd:/graphify", "bash:graphify", "hook:*graphify"},
		Moves:  []Metric{MetricToolKB, MetricCtx},
	},
	{
		ID: "tokensave", Name: "tokensave", Kind: KindRetrieval, URL: "https://github.com/aovestdipaperino/tokensave",
		About: "a code-graph MCP server in Rust, with hooks that steer exploration to it",
		Install: []Recipe{
			{Needs: "brew", Steps: []Step{{Argv: []string{"brew", "install", "aovestdipaperino/tap/tokensave"}, Skip: "tokensave"}, step("tokensave", "install", "--agent", "claude")}},
			{Needs: "cargo", Steps: []Step{{Argv: []string{"cargo", "install", "tokensave"}, Skip: "tokensave"}, step("tokensave", "install", "--agent", "claude")}},
		},
		Remove: []Recipe{{Needs: "tokensave", Steps: []Step{step("tokensave", "uninstall")}}},
		Detect: Detect{Bins: []string{"tokensave"}, MCP: []string{"tokensave"}, Hooks: []string{"tokensave"}, Need: "mcp"},
		Uses:   []string{"mcp:tokensave", "hook:*tokensave"},
		Moves:  []Metric{MetricToolKB, MetricCtx},
	},
	{
		ID: "context7", Name: "Context7", Kind: KindRetrieval, URL: "https://github.com/upstash/context7",
		About: "current library docs on demand, instead of searching the web or reading node_modules",
		Install: []Recipe{{Needs: "claude", Steps: []Step{
			step("claude", "mcp", "add", "--scope", "user", "--transport", "http", "context7", "https://mcp.context7.com/mcp"),
		}}},
		Remove: []Recipe{{Needs: "claude", Steps: []Step{step("claude", "mcp", "remove", "--scope", "user", "context7")}}},
		Detect: Detect{MCP: []string{"context7"}},
		Uses:   []string{"mcp:context7", "bash:ctx7"},
		Moves:  []Metric{MetricToolKB},
	},
	{
		ID: "bash-output", Name: "Shorter shell output", Kind: KindToolOutput,
		About:   "Claude Code keeps at most this many characters of a command's output; the rest is saved to a file it can read if it needs to",
		Setting: &Setting{Key: "bashOutputMaxChars", Value: 15000, Default: "30,000"},
		Moves:   []Metric{MetricToolKB, MetricCtx},
	},
	{
		ID: "mcp-output", Name: "Shorter MCP output", Kind: KindToolOutput,
		About:   "the most an MCP tool's answer may take up",
		Setting: &Setting{Key: "MAX_MCP_OUTPUT_TOKENS", Env: true, Value: "10000", Default: "25,000 tokens"},
		Moves:   []Metric{MetricToolKB},
	},
	{
		ID: "autocompact", Name: "Compact sooner", Kind: KindContext,
		About:   "compact at 400k tokens rather than near the end of a 1M window: every request re-reads the whole context, so a fat one costs on every turn",
		Setting: &Setting{Key: "autoCompactWindow", Value: 400000, Default: "the model's window"},
		Moves:   []Metric{MetricCtx, MetricCost},
	},
	{
		ID: "subagent-model", Name: "Sonnet for subagents", Kind: KindContext,
		About:   "subagents (Explore, general-purpose) run on Sonnet unless they ask for another model",
		Setting: &Setting{Key: "CLAUDE_CODE_SUBAGENT_MODEL", Env: true, Value: "sonnet", Default: "the session's model"},
		Moves:   []Metric{MetricCost},
	},
	{
		ID: "history", Name: "Keep 90 days of transcripts", Kind: KindMeasure,
		About:   "Claude Code deletes transcripts after 30 days; keeping them longer lets these graphs reach further back",
		Setting: &Setting{Key: "cleanupPeriodDays", Value: 90, Default: "30 days"},
	},
	{
		ID: "ccusage", Name: "ccusage", Kind: KindMeasure, URL: "https://github.com/ccusage/ccusage",
		About:  "usage reports from the same transcripts, by day, week, month and session",
		Manual: []string{"npx ccusage@latest daily"},
		Detect: Detect{Bins: []string{"ccusage"}},
	},
	{
		ID: "claude-monitor", Name: "Claude Code Usage Monitor", Kind: KindMeasure, URL: "https://github.com/Maciek-roboblog/Claude-Code-Usage-Monitor",
		About:   "a live terminal view of the 5-hour window's burn rate",
		Install: []Recipe{{Needs: "uv", Steps: []Step{step("uv", "tool", "install", "claude-monitor")}}},
		Remove:  []Recipe{{Needs: "uv", Steps: []Step{step("uv", "tool", "uninstall", "claude-monitor")}}},
		Detect:  Detect{Bins: []string{"claude-monitor"}},
	},
	{
		ID: "headroom", Name: "headroom", Kind: KindToolOutput, URL: "https://github.com/headroomlabs-ai/headroom",
		About:  "a local proxy that compresses tool output, logs and JSON before they reach the API",
		Claim:  "about 20% fewer tokens for coding agents",
		Note:   "works as a proxy through ANTHROPIC_BASE_URL: nothing shows in transcripts, and MCP tool search is turned off",
		Manual: []string{`uv tool install --python 3.13 "headroom-ai[all]"`, "headroom wrap claude"},
		Detect: Detect{Bins: []string{"headroom"}},
	},
	{
		ID: "claude-code-router", Name: "claude-code-router", Kind: KindContext, URL: "https://github.com/musistudio/claude-code-router",
		About:  "routes Claude Code's requests to other, cheaper models",
		Note:   "a proxy through ANTHROPIC_BASE_URL; costs here then no longer match what you pay",
		Manual: []string{"npm install -g @musistudio/claude-code-router", "ccr code"},
		Detect: Detect{Bins: []string{"ccr"}},
	},
}

// Find is the catalog's saver with this ID.
func Find(id string) *Saver {
	for i := range Catalog {
		if Catalog[i].ID == id {
			return &Catalog[i]
		}
	}
	return nil
}

// Matches is whether a use recorded in a transcript is this saver's.
func (s *Saver) Matches(key string) bool {
	for _, u := range s.Uses {
		kind, pat, _ := strings.Cut(u, ":")
		k, v, _ := strings.Cut(key, ":")
		if kind != k {
			continue
		}
		if len(pat) > 0 && pat[0] == '*' {
			if strings.Contains(v, pat[1:]) {
				return true
			}
		} else if v == pat || strings.HasPrefix(v, pat+":") || strings.HasPrefix(v, pat+" ") {
			return true
		}
	}
	return false
}
