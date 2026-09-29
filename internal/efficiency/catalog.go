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
	// else measured, when anyone has: shown as such, never as rush's.
	Claim, Measured string
	// Note is a caution shown before installing.
	Note string
	// Cut is what it can be expected to save, for the estimate against
	// your own figures.
	Cut *Cut

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
	// Exact is a setting that's only on at Value: any other is a choice
	// that isn't this saver.
	Exact bool
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

// Catalog is rush's curated list. Install commands are the projects' own
// documented ones, checked September 2026. Cuts are from independent
// measurements where there are any, the authors' own where not, and say
// which; retrieval savers' per-question figures compare against reading
// every file, so their cuts are well below them.
var Catalog = []Saver{
	{
		ID: "rtk", Name: "rtk", Kind: KindToolOutput, URL: "https://github.com/rtk-ai/rtk",
		About:    "Rust Token Killer: a hook rewrites Claude's shell commands (git, ls, tests, builds) so their output comes back compressed",
		Claim:    "60–90% less shell output",
		Measured: "JetBrains, 86 tasks: cost flat at high effort, +7.6% at low; only a third of shell calls were in its scope. codepointer, 614M tokens of real sessions replayed: −0.5% of the bill",
		Cut:      &Cut{Of: BaseShell, Low: 0, High: 0.05, Basis: "Measured, it barely moves the bill (−0.5% to +7.6%): the output it cuts is cached context, the cheapest tokens, and it can cost extra turns."},
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
		Measured: "JetBrains, 82 tasks: −8.5% output tokens, same quality; output is a small share of most bills",
		Cut:      &Cut{Of: BaseText, Low: 0, High: 0.085, Basis: "JetBrains measured −8.5% of output tokens against the 65% claimed."},
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
		Cut:   &Cut{Of: BaseToolOut, Low: 0, High: 0.15, Basis: "Not measured on its own; tools that shrink tool output took 0.5–2.8% off whole bills in codepointer's replay, about a sixth of the tool output they act on at most."},
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
		Cut:   &Cut{Of: BaseLook, Low: 0, High: 0.3, Basis: "No saving measured end to end; code-graph tools that were (codegraph, 7 repos) cut cost 0–78% depending on the repo."},
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
		About:    "builds a knowledge graph of the repo (tree-sitter, no LLM for code) and hooks steer Claude's searches and reads to it",
		Claim:    "71.5× fewer tokens per question, on a 52-file corpus against reading every file; ~1× on 6 files",
		Measured: "KubeBlogs, 6 real feature sessions: no session-level saving; cost followed how work was split among subagents",
		Note:     "its full skill is about 10k tokens when invoked, and the graph goes stale without its git hooks",
		Cut:      &Cut{Of: BaseLook, Low: 0, High: 0.25, Basis: "The one test on real work found no saving; per-question figures compare against reading every file, which Claude doesn't do."},
		Install: []Recipe{{Needs: "uv", Steps: []Step{
			{Argv: []string{"uv", "tool", "install", "graphifyy"}, Skip: "graphify"},
			step("graphify", "install"),
		}}},
		Remove: []Recipe{{Needs: "graphify", Steps: []Step{step("graphify", "uninstall")}}},
		Detect: Detect{Bins: []string{"graphify"}, Hooks: []string{"graphify hook-guard", "graphify"}, Files: []string{"skills/graphify/SKILL.md"}, Need: "hook"},
		Uses:   []string{"skill:graphify", "cmd:/graphify", "bash:graphify", "hook:*graphify"},
		Moves:  []Metric{MetricToolKB, MetricCtx},
	},
	{
		ID: "tokensave", Name: "tokensave", Kind: KindRetrieval, URL: "https://github.com/aovestdipaperino/tokensave",
		About:    "a code-graph MCP server in Rust (tree-sitter, 50+ languages, no LLM), with hooks that steer searches to it",
		Claim:    "88% fewer tokens to find code, on 10 questions about its own repo",
		Measured: "an independent run of its own bench: 94%; retrieval alone, no end-to-end test",
		Note:     "its tool schemas are ~100 KB (TOKENSAVE_TOOLS=core: ~19 KB), deferred by tool search unless a proxy turns that off; sends a token counter home unless `tokensave disable-upload-counter`",
		Cut:      &Cut{Of: BaseLook, Low: 0, High: 0.4, Basis: "No end-to-end test; code-graph MCPs that were (codegraph, 7 repos) cut cost 44% on average, 0–78% by repo."},
		Install: []Recipe{
			{Needs: "brew", Steps: []Step{{Argv: []string{"brew", "install", "aovestdipaperino/tap/tokensave"}, Skip: "tokensave"}, step("tokensave", "install", "--agent", "claude")}},
			{Needs: "cargo", Steps: []Step{{Argv: []string{"cargo", "install", "tokensave"}, Skip: "tokensave"}, step("tokensave", "install", "--agent", "claude")}},
		},
		Remove: []Recipe{{Needs: "tokensave", Steps: []Step{step("tokensave", "uninstall")}}},
		Detect: Detect{Bins: []string{"tokensave"}, MCP: []string{"tokensave"}, Hooks: []string{"tokensave hook-pre-tool-use", "tokensave"}, Need: "mcp"},
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
		Cut:     &Cut{Of: BaseFat, Low: 0.5, High: 0.9, Basis: "Compacted at 400k, the context past it isn't carried; summarising and reading files again takes some of that back."},
		Moves:   []Metric{MetricCtx, MetricCost},
	},
	{
		ID: "subagent-model", Name: "Sonnet for subagents", Kind: KindContext,
		About:   "subagents (Explore, general-purpose) run on Sonnet unless they ask for another model",
		Setting: &Setting{Key: "CLAUDE_CODE_SUBAGENT_MODEL", Env: true, Value: "sonnet", Default: "the session's model"},
		Note:    "a subagent that names its own model keeps it; CLAUDE_CODE_SUBAGENT_MODEL_FORCE overrides even that",
		Cut:     &Cut{Of: BaseSubBig, Low: 0.35, High: 0.6, Basis: "Sonnet 5 costs half of Opus 5.5 and a fifth of Fable per token; it may take more turns. No controlled study."},
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
		About:    "a local proxy that compresses tool output, logs and JSON before they reach the API",
		Claim:    "about 20% fewer tokens for coding agents",
		Measured: "codepointer, 614M tokens of real sessions replayed: −2.8% of the bill",
		Note:     "works as a proxy through ANTHROPIC_BASE_URL: nothing shows in transcripts, and MCP tool search is turned off unless ENABLE_TOOL_SEARCH is set",
		Cut:      &Cut{Of: BaseToolOut, Low: 0, High: 0.15, Basis: "codepointer measured −2.8% of a whole bill; one paper found tool-output cuts raising cost 6.8%."},
		Manual:   []string{`uv tool install --python 3.13 "headroom-ai[all]"`, "headroom wrap claude"},
		Detect:   Detect{Bins: []string{"headroom"}},
	},
	{
		ID: "claude-code-router", Name: "claude-code-router", Kind: KindContext, URL: "https://github.com/musistudio/claude-code-router",
		About:  "routes Claude Code's requests to other, cheaper models",
		Note:   "a proxy through ANTHROPIC_BASE_URL; costs here then no longer match what you pay, and MCP tool search is off unless ENABLE_TOOL_SEARCH is set",
		Manual: []string{"npm install -g @musistudio/claude-code-router", "ccr code"},
		Detect: Detect{Bins: []string{"ccr"}},
	},
	{
		ID: "codegraph", Name: "codegraph", Kind: KindRetrieval, URL: "https://github.com/colbymchenry/codegraph",
		About:  "a code-graph MCP server that Claude asks where things are and what calls what, instead of grepping and reading",
		Claim:  "44% lower cost, 62% fewer tokens: 7 repos, 4 runs each, with and without",
		Note:   "the authors found ~80% more retrieved context still held at the end of a session; about even on a Go repo",
		Cut:    &Cut{Of: BaseLook, Low: 0, High: 0.44, Basis: "Its authors' own with-and-without test: cost −44% on average, 0–78% by repo; not yet repeated by anyone else."},
		Manual: []string{"curl -fsSL https://raw.githubusercontent.com/colbymchenry/codegraph/main/install.sh | sh", "codegraph install"},
		Detect: Detect{Bins: []string{"codegraph"}, MCP: []string{"codegraph"}, Need: "mcp"},
		Uses:   []string{"mcp:codegraph"},
		Moves:  []Metric{MetricToolKB, MetricCtx, MetricCost},
	},
	{
		ID: "codebase-memory", Name: "codebase-memory-mcp", Kind: KindRetrieval, URL: "https://github.com/DeusData/codebase-memory-mcp",
		About:    "a code knowledge graph as an MCP server, with skills and hooks",
		Claim:    "120× fewer tokens, on 5 questions",
		Measured: "its paper, 31 repos: 10× fewer tokens, but 83% of answers right against 92% reading files",
		Note:     "cheaper answers were also worse ones in its own paper",
		Cut:      &Cut{Of: BaseLook, Low: 0, High: 0.5, Basis: "Its paper measured 10× fewer tokens for questions, at 9 points of accuracy; no whole-session test."},
		Manual:   []string{"curl -fsSL https://raw.githubusercontent.com/DeusData/codebase-memory-mcp/main/install.sh | bash"},
		Detect:   Detect{MCP: []string{"codebase-memory-mcp"}, Bins: []string{"codebase-memory-mcp"}, Need: "mcp"},
		Uses:     []string{"mcp:codebase-memory-mcp"},
		Moves:    []Metric{MetricToolKB, MetricCtx},
	},
	{
		ID: "lsp", Name: "Language server plugins", Kind: KindRetrieval, URL: "https://code.claude.com/docs/en/costs",
		About:  "Anthropic's own plugins (gopls-lsp, typescript-lsp, pyright-lsp, rust-analyzer-lsp…) give Claude go-to-definition and references instead of grep then read",
		Note:   "each needs its language server on the PATH (gopls, typescript-language-server…)",
		Cut:    &Cut{Of: BaseLook, Low: 0, High: 0.2, Basis: "Recommended in Claude Code's cost guide, but not measured by anyone."},
		Manual: []string{"/plugin install gopls-lsp@claude-plugins-official", "/plugin install typescript-lsp@claude-plugins-official"},
		Detect: Detect{Plugins: []string{"gopls-lsp@claude-plugins-official", "typescript-lsp@claude-plugins-official", "pyright-lsp@claude-plugins-official", "rust-analyzer-lsp@claude-plugins-official"}},
		Moves:  []Metric{MetricToolKB},
	},
	{
		ID: "claude-context", Name: "claude-context", Kind: KindRetrieval, URL: "https://github.com/zilliztech/claude-context",
		About:  "semantic code search as an MCP server: the repo is embedded into a vector database",
		Claim:  "~40% fewer tokens at the same retrieval quality, on its own evaluation",
		Note:   "needs an OpenAI key (embedding costs its own tokens) and a Milvus or Zilliz database",
		Cut:    &Cut{Of: BaseLook, Low: 0, High: 0.4, Basis: "Its authors' own evaluation only."},
		Manual: []string{"claude mcp add claude-context -e OPENAI_API_KEY=… -e MILVUS_ADDRESS=… -- npx @zilliz/claude-context-mcp@latest"},
		Detect: Detect{MCP: []string{"claude-context"}},
		Uses:   []string{"mcp:claude-context"},
		Moves:  []Metric{MetricToolKB},
	},
	{
		ID: "ponytail", Name: "Ponytail", Kind: KindTerse, URL: "https://github.com/DietrichGebert/ponytail",
		About:    "a plugin that has Claude write less code: smaller diffs, fewer rewrites",
		Claim:    "−20% cost, −54% lines, on 12 tickets",
		Measured: "JetBrains, 80 tasks: −10.3% cost, −15% lines, same quality",
		Cut:      &Cut{Of: BaseAll, Low: 0.05, High: 0.10, Basis: "JetBrains measured −10.3% of cost across whole sessions (p=0.004)."},
		Install: []Recipe{{Needs: "claude", Steps: []Step{
			step("claude", "plugin", "marketplace", "add", "DietrichGebert/ponytail"),
			step("claude", "plugin", "install", "ponytail@ponytail"),
		}}},
		Remove: []Recipe{{Needs: "claude", Steps: []Step{step("claude", "plugin", "uninstall", "ponytail@ponytail")}}},
		Detect: Detect{Plugins: []string{"ponytail@ponytail"}},
		Uses:   []string{"skill:ponytail", "hook:*ponytail"},
		Moves:  []Metric{MetricOut, MetricCost},
	},
	{
		ID: "concise", Name: "Concise output style", Kind: KindTerse,
		About:   "Claude Code's own style that leads with the result and leaves out preamble, narration and recaps",
		Setting: &Setting{Key: "outputStyle", Value: "Concise", Default: "the default style", Exact: true},
		Cut:     &Cut{Of: BaseText, Low: 0, High: 0.085, Basis: "Not measured; the nearest, caveman, took 8.5% off output in JetBrains' test."},
		Moves:   []Metric{MetricOut},
	},
	{
		ID: "git-instructions", Name: "No git instructions", Kind: KindContext,
		About:   "leaves Claude Code's commit and PR instructions and the git status snapshot out of every session's start",
		Note:    "Claude still runs git, but without the house rules for commits and PRs",
		Setting: &Setting{Key: "includeGitInstructions", Value: false, Default: "included", Exact: true},
		Cut:     &Cut{Of: BaseStart, Low: 0.02, High: 0.06, Basis: "About 1–2k tokens of what sessions start with, read again by every request."},
		Moves:   []Metric{MetricStart},
	},
	{
		ID: "claude-mem", Name: "claude-mem", Kind: KindContext, URL: "https://github.com/thedotmack/claude-mem",
		About:  "remembers past sessions and primes new ones with short summaries, fetching detail only when asked",
		Note:   "hooks on every prompt and tool call, and now asks you to sign in to a hosted service by default; no measured saving",
		Manual: []string{"npx claude-mem install"},
		Detect: Detect{Plugins: []string{"claude-mem@thedotmack"}, Hooks: []string{"claude-mem"}},
		Uses:   []string{"hook:*claude-mem", "mcp:*claude-mem"},
		Moves:  []Metric{MetricStart, MetricCtx},
	},
	{
		ID: "claude-hud", Name: "claude-hud", Kind: KindMeasure, URL: "https://github.com/jarrodwatts/claude-hud",
		About:  "a status line showing how full the context is, as you work",
		Manual: []string{"/plugin marketplace add jarrodwatts/claude-hud", "/plugin install claude-hud"},
		Detect: Detect{Hooks: []string{"claude-hud"}},
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
