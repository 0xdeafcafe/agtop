# agtop for every agent

agtop is written for Claude Code throughout. This is the plan for making the core agent-agnostic, then adding Codex, Copilot CLI, Kimi CLI, Mistral Vibe and others as adapters. It has four stages: refactor, architecture, usage, then the agents themselves. The architecture comes first in this document because the refactor needs a shape to aim at.

## Where Claude Code is wired in today

- **Identity.** `claude.Account` (a `~/.claude*` config dir) is the account type everywhere: state, actions, daemon, efficiency, statusline and about 15 UI files. Logins are Claude OAuth credentials swapped in and out of the keychain.
- **The agent row.** `fleet.Agent` embeds `claude.Job`, which is Claude's background job file. Discovery reads `~/.claude/sessions/<pid>.json`, `jobs/*/state.json` and the daemon roster, and checks processes by `comm == "claude"`.
- **The event model.** `headless.Event` is already a typed layer between the wire and the renderer, and `convo.Session.Apply` is fed by both live sessions and JSONL transcripts through it. Its types still follow Claude's protocol, though: `Block{Type:"tool_use"}`, `Usage` with Anthropic's cache fields, `PermissionRequest` with `Suggestions`, and task types like `local_bash`.
- **The wire.** `agtop host` sends Claude's raw stream-json lines to every client (UI, menubar, plugind, `agtop session`), and each client decodes them itself.
- **Tools.** convo and efficiency switch on Claude's tool names (`Bash`, `Edit`, `TodoWrite`, `AskUserQuestion` and about 30 more) and read Claude's input keys (`command`, `file_path`) and structured results (`structuredPatch`, `stdout`).
- **Usage.** `claude.Usage` has exactly two windows, `FiveHour` and `SevenDay`. The UI, statusline, menubar, state (`SwitchAt`) and `--dump` all read them by name.
- **Features.** Rewind, fork, skills, hooks, plugins, the statusline hook, CLAUDE.md memory, `/permissions`, the Claude settings tab, the efficiency savers catalog and `agtop on` are all Claude features. Other agents have some of them, in other shapes.

Some of it is already neutral and needs no work: `proc`, `cellw`, `fswait`, most of `plugin`, the queue, idle-stop and retry logic in `host`, and most of the UI chrome.

## Architecture

### Four words, kept apart

| Word | Meaning | Examples |
|---|---|---|
| **Agent** (kind) | A program that does coding work, and the adapter that drives it. | `claude`, `codex`, `copilot`, `kimi`, `vibe`, `gemini` |
| **Profile** | One config home for one agent: where its settings, transcripts and sessions live. | `~/.claude`, `~/.codex`, `~/.copilot`, `CLAUDE_CONFIG_DIR=…` |
| **Account** | Who pays, and whose quota is used. A credential with a plan. | A Claude Max sign-in, a ChatGPT Plus sign-in, a GitHub Copilot seat, a Moonshot API key |
| **Session** | One conversation run by one agent, in one profile, on one account. | A live `claude -p`, a Codex thread, a past transcript |

Today `claude.Account` stands in for both Profile and Account, and Logins are a second, Claude-only kind of Account. Keeping the four apart is what makes usage work across agents: a Copilot seat can run Claude and GPT models, and one ChatGPT account can be signed into several Codex profiles.

### Packages

```
internal/agent/               the core domain. Imports nothing agent-specific.
  agent.go                    Kind, Adapter interface, Capabilities, registry
  profile.go                  Profile, Account, Credential
  session.go                  Session (live or past), State, Todo, Task, PR
  event/                      the neutral event model (replaces headless.Event on the wire)
  tool/                       ToolKind and typed inputs and results
  usage/                      Quota, Window, TokenUsage, Pricing, the shared cache (usagecache.go moves here)

internal/adapters/
  claude/                     today's internal/claude + internal/headless + internal/daemon, moved
  acp/                        a generic Agent Client Protocol client (JSON-RPC over stdio)
  codex/                      codex app-server driver, ~/.codex/sessions rollouts, ChatGPT rate limits
  copilot/                    ACP via `copilot --acp`, ~/.copilot, premium-request quota
  kimi/                       ACP via `kimi acp`, Moonshot usage
  vibe/                       ACP via `vibe-acp`, Mistral usage
  gemini/ …                   the same pattern; each is thin when ACP does the driving

internal/fleet, host, convo, ui, statusline, menubar, efficiency, plugind
                              depend only on internal/agent. They find adapters through the registry.
```

`cmd/agtop` imports every adapter for side effects (`_ "…/adapters/codex"`), which registers it, so the core never names one.

### The adapter interface

An adapter is a set of optional parts. The core asks for each one and hides the feature when it isn't there, rather than every adapter stubbing everything.

```go
package agent

type Adapter interface {
    Kind() Kind                      // "claude"
    Name() string                    // "Claude Code"
    Caps() Caps                      // what it can do, see below
    Profiles() []Profile             // the config homes it finds on this machine
}

// Optional parts, found with a type assertion.
type Discoverer interface {          // agents running outside agtop, and past ones
    Live(p Profile, procs *proc.Table) []Session
    Past(p Profile) []Session
}
type Driver interface {              // run a session headless, for agtop mode
    Start(ctx context.Context, o StartOptions) (Conn, error)
}
type Conn interface {
    Events() <-chan event.Event
    Send(Input) error
    Answer(req event.Approval, a Answer) error
    Interrupt() error
    SetModel(string) error
    SetMode(string) error
    Close() error
}
type HistoryReader interface {       // a transcript turned into the same events
    History(s Session, before time.Time) ([]event.Event, error)
    Tail(s Session) Tailer
}
type QuotaSource interface {         // see Usage below
    Quota(ctx context.Context, a Account) (usage.Quota, error)
}
type Accounts interface {            // sign in, keep, switch
    Accounts() []Account
    Current(p Profile) (Account, error)
    Switch(p Profile, a Account) error
    SignIn(p Profile) *exec.Cmd
}
type Pricer interface{ Price(model string) (usage.Price, bool) }
type Commands interface{ Commands(p Profile, cwd string) []Command }   // slash commands, skills
type Instructions interface{ Files(p Profile, cwd string) []DocFile }  // CLAUDE.md, AGENTS.md, …
```

`Caps` is a bit set for everything smaller than a whole interface: `Rewind`, `Fork`, `Resume`, `Images`, `Effort`, `PermissionModes`, `PlanMode`, `Subagents`, `BackgroundTasks`, `StructuredQuestions`, `ContextUsage`, `Compact`, `MCP`, `Hooks`, `Plugins`, `StatusLineHook`, `NativeScreen`. `sessionviews.go`'s command registry becomes `{command, needs Caps}`, so `/rewind` only shows for agents that can.

### The event model

`headless.Event` becomes `agent/event`, with the Claude-shaped parts taken out:

- `Init{SessionID, Model, Cwd, Mode, Version, Tools, Commands, MCP}`
- `MessageStart`, `Delta{Part, Kind: text|thinking|toolInput}`, `PartStart`
- `Message{Role, ID, Model, Parent, Parts []Part, Tokens *usage.TokenUsage}`
- `Part` is a sum: `Text`, `Thinking`, `ToolCall{ID, Name, Kind tool.Kind, Input tool.Input, Raw json.RawMessage}`, `ToolResult{CallID, Text, IsError, Result tool.Result, Raw}`, `Image`.
- `Approval{ID, Call ToolCall, Reason, Options []ApprovalOption}`. ACP's `session/request_permission` sends the options, and Claude's `can_use_tool` has allow, deny and always-allow; one list of options covers both.
- `Question{ID, Questions}`, split out from Claude's AskUserQuestion, so any agent with structured questions uses the same card.
- `TurnEnd{Reason, Err, Cost, Tokens, Duration}`, `Compacted{…}`, `QuotaUpdate{usage.Quota}`, `TaskStarted/Updated/Done` (with a neutral `TaskKind`: shell, subagent, monitor, workflow), `Plan{[]Todo}`, `Status`, `Other{Adapter, Raw}`.

Tools are normalised once, in the adapter:

```go
package tool
type Kind int  // Shell, Read, Edit, Write, Search, Glob, Fetch, WebSearch, Subagent,
               // Todo, Question, PlanMode, MCP, Notebook, Other
type Input struct {           // the common fields, filled by the adapter
    Command, Path, Pattern, Query, URL, Description string
    Edits []Edit              // old → new, for Edit, MultiEdit, apply_patch, ACP diffs
    Server, Tool string       // MCP
}
type Result struct { Stdout, Stderr string; Exit *int; Patches []Patch; Lines *LineSpan }
```

convo's `render.go` switches on `tool.Kind` rather than on `"Bash"`. The Claude-only extras (`SendMessage`, `ScheduleWakeup`, `EnterWorktree`, `Monitor`) stay as `Kind: Other` with the adapter's name, and get drawn by name the way they are now, so nothing that renders today is lost. ACP already sends a tool `kind` (read, edit, delete, move, search, execute, think, fetch) and diff content, so ACP adapters get this nearly for free.

### The host

`agtop host` runs a `Driver` instead of `headless.Session`, and sends **neutral events** to clients (JSON with a `"t"` type tag), not Claude's lines. The replay ring stores the same neutral events. This is the one wire change: it bumps `Proto` to 4. An older host keeps working until its session idles out, because the client still reads Proto ≤3 lines through the Claude adapter's decoder.

The host's own logic stays agent-neutral: queue, idle stop and resume, retries, branches, pending approvals. The Claude-specific pieces (usage-limit parsing, the `continue` after a reset, `ownTraffic` filtering, the checkpoint env) move behind the Claude driver. Wherever the host matches on error text, it now asks the driver: `Classify(err) → {Retryable, Auth, TooLong, Limited(until)}`.

### Discovery

`fleet.Loader.Load` loops over `registry × profiles` and calls `Discoverer.Live` and `Past`, then adds agtop-hosted sessions from `host.List()` as it does now. `fleet.Agent` stops embedding `claude.Job` and holds `agent.Session` plus the fleet fields (CPU, memory, pins, group). Each adapter does its own process matching; `isClaudePID` moves into the Claude adapter.

## Usage

### The model

```go
package usage

type Window struct {
    ID       string        // "five_hour", "seven_day", "seven_day_opus", "primary", "month"
    Label    string        // "5h", "week", "Opus week", "premium requests"
    Span     time.Duration // 5h, 7d, 30d, 0 when the provider doesn't say
    Percent  float64       // 0–100, always filled, derived from Used/Limit when those are sent
    Used, Limit float64    // when the provider counts (Copilot: 212 of 300 requests)
    ResetsAt time.Time
    Scope    Scope         // which models it applies to; zero means all
}
type Scope struct{ Models []string } // e.g. Claude's Opus week, or a per-model Codex limit

type Quota struct {
    Account   AccountKey    // "claude:login:<uuid>", "codex:chatgpt:<id>", "copilot:gh:<login>"
    Plan      string
    Windows   []Window
    Credits   *Money        // prepaid or overage balance, if any
    FetchedAt time.Time
    Source    Source        // live event, agtop fetch, agent's own cache
    Problem   string
}

func (q Quota) Tightest(model string) (Window, bool)  // the window that stops this model first
```

- **Windows are a list**, so Claude's 5h and week, Claude's Opus week, Codex's primary and secondary (whose lengths come from `windowDurationMins`, not from position), and Copilot's monthly premium requests all fit without new fields.
- **A quota belongs to an Account, not to an agent or a model.** Sessions point at an Account. When two agents share one account (Claude Code and Copilot both on one GitHub seat, or several Codex profiles on one ChatGPT sign-in), they share one Quota and one line in the Accounts view.
- **Model scope.** A window with a `Scope` only counts for those models. `Tightest(model)` picks the window that decides whether a session can carry on: an Opus session on Claude reads the Opus week and the 5h window, a Sonnet session skips the Opus week. The auto-switch and the top-bar meter both use it, instead of `max(FiveHour, SevenDay)`.
- **Per-request multipliers.** Copilot charges premium requests per model (a multiplier each). `Pricer` returns either a price in dollars or a request multiplier, so "what did this session cost" reads "$3.20" on Claude and "14 premium requests" on Copilot.
- **Tokens.** `TokenUsage{Input, Output, CacheRead, CacheWrite map[ttl]int, Reasoning}`, where each adapter fills in what its provider reports. Cost is always computed by the adapter's `Pricer`, so efficiency stops pricing everything as Claude.

### Where readings come from

There are three sources, as today, but each adapter supplies its own:

| Agent | Live (from the session) | Fetched (by agtop) | Agent's own cache |
|---|---|---|---|
| Claude Code | `rate_limit_event` | `api.anthropic.com/api/oauth/usage` | `.claude.json` |
| Codex | `token_count` / rate-limit notifications | `account/rateLimits/read` over `codex app-server` | rollout files |
| Copilot | none known yet | GitHub premium-request usage API | none |
| Kimi / Vibe / API-key agents | per-turn tokens only | provider billing API, if any | none |

The cache in `usagecache.go` (flock'd `usage.json`, backoff) is already keyed by string and takes a fetch function, so it moves to `internal/agent/usage` almost unchanged, keyed by `AccountKey`.

### Switching accounts

`fleet.NextLogin` becomes `usage.Next(accounts, model, stopped)`. It is generic over any adapter whose `Accounts` can switch, and it only switches between accounts of the same adapter. Moving a conversation to another agent is a different feature (see Later). `state.SwitchAt` applies to `Tightest(model)`.

## The refactor, in steps that each build and commit

Each step keeps behaviour identical. The golden tests in `convo` and the frame tests in `ui` are the safety net, and every step runs them.

1. **Neutral types, aliased.** Create `internal/agent` and `internal/agent/usage`, and move the types that are already generic into them: `Todo`, `Task`, `PR`, `SubagentStats`, `Preview`, `Convo`, `Command`, `TokenUsage`, `Totals`, and the usage cache. Leave `type X = agent.X` aliases in `internal/claude` so nothing else changes yet.
2. **Usage as a list.** Replace `claude.Usage{FiveHour, SevenDay}` with `usage.Quota{Windows}`, and add `Tightest`. Update ui, statusline, menubar, `--dump` and logins. Claude fills in `five_hour`, `seven_day` and `seven_day_opus` (the last is parsed today but not shown).
3. **Profile and Account.** Add `agent.Profile{Kind, Dir, Name}` and `agent.Account`, and change `state.Config.Accounts` to profiles with a `kind` (a JSON migration: old entries become `kind: claude`). Replace `claude.Account` in function signatures, and turn `Login` and `Vault` into the Claude adapter's `Accounts`.
4. **fleet.Agent off claude.Job.** Add `agent.Session` with the generic Job fields, and move `CLIVersion`, `RespawnFlags` and `Worker` into an adapter-owned `Extra any`. Move discovery into the Claude adapter's `Discoverer`.
5. **Tools normalised.** Add `tool.Kind` and `tool.Input` and fill them in the Claude decoder. Move `render.go`, `changes.go`, `commits.go`, `messages.go`, `efficiency/scan.go` and `claude.Doing` onto them, one file per commit, with the golden tests unchanged.
6. **Neutral events.** Rename `headless.Event` to `agent/event` with the shapes above. The Claude decoder produces them, and convo, plugind and menubar consume only them.
7. **The host speaks neutral events.** Put a `Driver` interface behind `host`, make `headless.Session` the Claude driver, move the wire to Proto 4, and keep reading Proto ≤3.
8. **Capabilities.** Add `Caps` gates on commands, sheets and tabs (rewind, fork, skills, plugins, hooks, statusline, memory, the Claude settings tab, efficiency savers, logins).
9. **Move.** Move `internal/claude`, `internal/headless` and `internal/daemon` to `internal/adapters/claude/…`, and add a lint check (`go list -deps`) that fails if a core package imports an adapter.

Steps 1–3 are mostly mechanical and quick. Steps 5–7 are the real work.

## Adding the agents

Each agent is its own milestone: it appears in the list, runs in agtop mode, draws its history, and shows its usage.

1. **ACP client** (`adapters/acp`). JSON-RPC over stdio: `initialize`, `session/new`, `session/load`, `session/prompt`, `session/update` notifications (agent message chunks, thought chunks, tool calls with kind and diffs, plan entries), `session/request_permission`, `session/cancel`, `session/set_mode`, and the client-side `fs/*` and `terminal/*` methods. It maps all of this onto `agent/event`. Every ACP agent gets a Driver from this one package.
2. **Codex.** Use the native `codex app-server` (JSON-RPC) rather than ACP. It exposes rate limits, reasoning effort, approvals and thread resume directly, and the usage story needs the rate limits. The adapter also covers history from `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl`, profiles from `CODEX_HOME`, and discovery by process name `codex`.
3. **Copilot CLI.** A Driver through ACP (`copilot --acp`), profiles from `~/.copilot` (or `COPILOT_HOME`), history from its session-state files, and a quota of monthly premium requests from GitHub's API, with a model multiplier in `Pricer`.
4. **Kimi CLI** (`kimi acp`) and **Mistral Vibe** (`vibe-acp`). Thin adapters on top of `acp`, plus history readers for their session files, and quota from their billing APIs where one exists. Otherwise the adapter reports token spend only, with no windows.
5. **Gemini CLI, OpenCode, Goose.** The same ACP pattern, added when someone wants them.

For each one, before writing its history reader and quota source, check its on-disk session format and usage endpoint against the current release, since they change often.

### What an ACP-only agent can't do (yet)

Rewind, fork, file checkpoints, context-usage breakdowns, background task control, and the native "screen" tab have no ACP equivalent. Those features stay capability-gated and hidden for these agents rather than faked.

## Later

- **Hand a conversation to another agent** ("carry on in Codex"): render the transcript to a prompt and start a new session. This only makes sense once two agents run.
- **Plugins across agents.** `plugin.Contributions.Flags` emits Claude flags today. Tools already go through MCP, which ACP agents accept (`mcpServers` in `session/new`), so plugin tools can travel. Subagent and system-prompt contributions stay Claude-only.
- **`agtop on`** for other agents' commands (`codex`, `copilot`), if they grow a view worth replacing.
