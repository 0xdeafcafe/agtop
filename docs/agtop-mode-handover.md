# agtop mode: handover

Written 23 Sep 2026, at the end of a long build session, before a context compaction. It's for whoever picks this up next, human or agent. Everything below is committed on `main` (local, not pushed unless noted).

## What agtop mode is

agtop runs Claude Code **headless** (`claude -p --input-format stream-json --output-format stream-json --permission-prompt-tool stdio`). It does that inside a small detached Go process per session, `agtop host run <id>`, and draws the whole session itself.

Agents that Claude Code's own daemon runs are drawn by the **same renderer, from their transcript files**. So every agent's right-hand pane (the **Session**) looks and works the same.

**Design spec** (user-approved, round 3.1): https://claude.ai/artifact/2pdtkfBi4he8cVWra7qYq6. The source HTML lived in a job temp folder and may be gone; the published artifact is the reference.

## Decisions the user made (don't relitigate)

**Design direction**
- Claude Code's UI is inspiration only; design in agtop's own language. Round 1 copied Claude Code and was rejected.
- Surfaces over rules, visible elevation, and a bordered input box that is obviously where you type.

**Layout and focus**
- The list (**Agents**) keeps ≥25% width (≥30 columns). There's no split when the pane would be under 84 columns. The user can resize (shift+←/→ or alt+←/→ with nothing typed, drag the divider, `/width 30%`). `#view split|agent|list` picks the layout outright. Stepping or dragging past either end leaves one side alone: past 75% is Agents alone (`Config.ListOnly`, kept across restarts; past 25% is the Session alone (`m.full`); the opposite step brings the split back. From Agents alone, a Session opens as you last had one (`Config.ChatFull`, see `chatAlone()`): push it alone and later ones open alone, split it and they open beside the list. Closing (esc, ←) goes back to Agents alone; hiding (past 75%, `#view list`) is what sets it.
- Names for the parts:
  - **Agents** (left list)
  - **Session** (right side), with views **conversation · overview · changes · subagents** (· screen for live Claude Code agents, which the user wants removed, see below)
  - **Prompt** (left box: new sessions and `/` commands only)
  - **Message box** (Session box: talks to that agent)
  - **Dock** (raised strip: task, cards, queue, message box)
- One box per destination; never two boxes on screen for the same agent.
- Focus is visible: the unfocused side fades (SGR dim), and the focused side has an orange ▍, title and box edge.
- enter/→ goes into a Session. esc/← goes back. ← climbs step → turn → Agents. Clicks focus and select, and the wheel scrolls whatever is under the pointer.

**Keys**
- Letters always type. Remap applied:
  - ctrl+r renames (or #rename)
  - `/done` and `/group`
  - ctrl+l: folder picker while drafting, else move the agent
  - ctrl+n: next agent needing you
  - ctrl+k/ctrl+j are editor keys
- esc never interrupts; ctrl+x stops a turn (twice stops the session).
- Cards (approvals, questions, the usage-limit question) answer **only** after ↑ focuses the card, or with alt+y/alt+a/alt+n. Never from the first letter or digit of a message. This was a review finding, fixed in 52ed8b2.

**Behaviour**
- New sessions run in agtop mode by default (`Dispatch.RunIn` "" = agtop, "daemon" = old). `/agtop` moves a running Claude Code session over, conversation intact (verified live).
- Usage limits: opt-in per session by default (setting auto / opt-in / off). API errors retry with doubling waits, capped by the 1-hour prompt cache (sessions write `ephemeral_1h`).
- Queue: the whole queue sends as one message by default. Keep reorder and edit, and add a "separately" switch. **The UI for this is not built yet.**
- The changes view shows both this session's edits and the working tree (git), marked by whether this session touched each file.
- Cold starts that are expected (session start, each new subagent, model switch, idling past the cache's hour) must not look alarming.
- The user works on `main` directly. No worktrees or branches. `.claude/settings.json` has `worktree.bgIsolation: "none"` (uncommitted, user-created).

## Architecture and files

| Package / file | What | Owner |
|---|---|---|
| `internal/headless` | stream-json protocol types (`event.go`) + process wrapper (`session.go`): send/sendWith images, allow/deny, interrupt, set mode/model, initialize (slash commands), stop | this work |
| `internal/host` | per-session detached host (`host.go`) + client (`client.go`): replay ring, time stamps, queue ops, approvals, idle stop/resume, usage-limit continue, API retries, effort | this work |
| `internal/convo` | the conversation model (`model.go`); renderer (`render.go`); overview (`overview.go`); transcript tail (`transcript.go`); subagents (`subagents.go`); changes (`changes.go`); search (`search.go`) | this work |
| `internal/ui/agtopmode.go` | Session pane: connections (host or transcript tail), header, views, dock, cards, keys, search keys, subagents view, `/agtop` | this work |
| `internal/ui/editor.go` | line editor (`edit`, `editSel` with selection), image path parsing, chips | this work |
| `internal/ui/inputbox.go` | the bordered box; own word wrap with offsets (`wrapSegs`), block cursor, selection highlight, click mapping (`at`) | this work |
| `internal/agtools` | agtop's own MCP tools (`mcp__agtop__*`), served in-process by the host: Claude Code names the server in `initialize.sdkMcpServers` and sends each JSON-RPC message as a `mcp_message` control request (`headless.MCPRequest` → `agtools.Handle` → `Session.ReplyMCP`). Tools carry `_meta["anthropic/alwaysLoad"]` so they aren't behind tool search, and are passed to `--allowedTools`; the host keeps their control traffic out of the ring. `show` draws a figure (`convo` `figure`/`Drawing`). A tool that must wait on the user would answer `tools/call` later, from the UI, via a new host op | this work |
| `internal/ui/claudetab.go` | Settings › Claude tab (settings.json + env) | this work |
| `internal/ui/zen.go` | Zen view | this work |
| `internal/claude/settings.go` | settings.json reader/writer | this work |
| `internal/ui/view.go`, `keys.go`, `model.go`, `dialog.go`, `live.go`, `fleet/fleet.go` | shared with another session ("claude code agent wrapper", now exited) that built the list, header, settings, accounts and the old preview. My hooks: `layout`/`sideWidth`/`colWidths`/`promptLines`/`side`/`divider`/`claudeStrip`, key remap, `fleet.hosted()` | shared |

**How data flows**
- **agtop session:** `claude -p` → headless → host (ring + info.json at `~/.config/agtop/sessions/<id>/`) → unix socket → `host.Client` → `host.Decode` → `convo.Session.Apply` → `Render`/`Overview`/…
- **Claude Code session:** transcript `~/.claude/projects/<slug>/<sid>.jsonl` → `convo.Tail.Read` (incremental) → the same `convo.Session`. Subagents: `<sid>/subagents/agent-<id>.jsonl` + `.meta.json`.

## Commits (newest first)

- `52ed8b2` no accidental answers/sends (card focus, zen guard, one box per destination)
- `31e06e6` real editor: selection, copy, click-to-place, → fix
- `3745266` shell turns, bold-white specials, links, chips
- `36e14a6` Zen
- `96d8f53` crash fix (nil client), changes view, ctrl+f search
- `13fdbcd` subagents view, cold-start reasons
- `a0388d4` one Session for every agent (transcripts)
- `db706f7` calmer conversation, selection scroll/click/←
- `891cd4a` Settings › Claude tab
- `36cb331` questions + images
- `88ecbbf` limits + retries
- `c8aff35`, `9807c1f` agtop mode in the UI
- `54927bd`, `49720ad`, `62cc86f` host + headless
- `8921bd4`, `2cec322` convo renderer + overview
- `cb617c7` live terminal preview

## Work left, in priority order

### 1. Correctness bugs from the review (not fixed yet)

**Host state machine** (`internal/host/host.go`):
- [x] An auto-continue limit can freeze the queue for good. `busy` stays true while `Limit.Continue` is set, but `sendLocked` stops the timer, a limit with no `ResetsAt` never schedules one, and the `limit` op can leave `Continue` set with no timer. Clear `Limit` on send and on a successful Result. **Done (d834b1e).**
- [x] The limit text match (`"usage limit"`, `"limit reached"`) runs before the `IsError` check, so a successful answer mentioning it counts as a limit. Only match when `IsError`. Reset `limitRaw` after a successful turn. **Done (d834b1e).**
- [x] Sends during an idle stop or effort stop are lost: `mu` is released while `s.sess` still points at the dying process. Set `s.sess = nil` under `mu` before calling `Stop`. **Done (d834b1e).**
- [x] A retry/continue timer can fire after the user has already sent (`AfterFunc` waiting on `mu`). Add a generation counter checked inside `after`. **Done (d834b1e).**
- [x] A double `stop` panics on `close(s.quit)`. Use `sync.Once`. **Done (d834b1e).**
- [x] Up to ~7 MB of image base64 is written to stdin while holding `mu` (risk of deadlock). Write outside the lock. **Done (d834b1e: stdin writes go through a writer goroutine and never block the lock).**
- [x] State never returns to "working" when Claude starts another turn after a Result (e.g. background-task wakeups). Set working on `Status`/`MessageStart` events. **Done (d834b1e).**
- [x] Queue ops are addressed by index, which races with the automatic pop. Give queue items ids. **Done (d834b1e: ops carry the text they saw and find it if the queue moved).**
- [x] A popped queue item is lost if its send fails. Re-queue it. **Done (d834b1e).**
- [x] The queue still drains after an interrupt. Hold on interrupt. **Done (d834b1e).**
- [x] An over-long line: the scanner's 64 MB buffer can make `Wait` hang. Surface the error and restart. **Done (d834b1e: the process is killed and the error surfaced).**
- [x] **The whole queue should send as one message by default** (the user agreed). The host currently pops one item per turn. **Done (6ffc261).**

**Convo** (`internal/convo`):
- [x] **Phantom live turn** (fixed after the handover: `turnFor(now)` sets `Start`, tool results go to their step's own turn, and a turn with no prompt reads "picked up on its own"). Was: `turnFor()` created a Live turn with zero `Start`, so the header shows "✻ working 2562047h" (seen on real sessions). Set `Start: now`, and route messages with a parent and tool_results to their step's own turn rather than opening a new one.
- [ ] Subagent output leaks into the main turn when its parent step is unknown (checks `parent != nil`, should check `ParentToolUseID != ""`). It also overwrites `Context` and the turn's model.
- [ ] **Totals are wrong:**
  - [x] `request()` only merged a repeat of the *last* message id (fixed in `0a2a077`: map by id, max output tokens, `<synthetic>` skipped).
  - Output tokens come from partial stream snapshots (5k vs 224k). Keep the max per id.
  - Skip `<synthetic>` model messages.
  - Together these cause the false "cache dropped early · 0.5s" cold starts.
- [ ] Opening a folded run shows only its first step and refolds the rest (`render.go`, the run logic). When a run's ref is open, render all of `items[i:j]` and skip past them.
- [ ] `isRejection` matches any "permission" text, so EACCES failures show as denied. Tighten it.
- [ ] "[Request interrupted by user]" becomes a new turn and the interrupted turn shows ✓. Mark the turn stopped instead.
- [ ] A truncated or rewritten transcript replaces `t.Sess`, but the UI keeps the old pointer and freezes. Reset the Session in place.
- [ ] Tabs, CRLF and non-SGR escapes in Bash output break widths and reach the terminal. Use `ansi.Strip` and expand tabs everywhere in output.
- [ ] The `Exit code N` regex matches any line of a successful command's output. Only parse it when `IsError`.

**settings.json writer** (`internal/claude/settings.go`, `ui/claudetab.go`); **real bug, fix before anyone uses the Claude tab on real settings**:
- [x] Values are HTML-escaped on save: `json.Marshal` of a `RawMessage` escapes `<>&`, so hook commands like `a && b > x` get mangled. Write with `SetEscapeHTML(false)`, or `json.Indent` the raw bytes. **Done (6cdbf9e).**
- [x] Keys get re-sorted. Keep the original order (decode with `json.Decoder` tokens). **Done (6cdbf9e).**
- [x] A symlinked settings.json (dotfiles) is replaced by a plain file. Save to `filepath.EvalSymlinks(path)`. **Done (6cdbf9e).**
- [x] Changes made in the meantime by Claude Code's `/model` or `/config` are overwritten. Re-read and apply only the changed keys just before saving. **Done (6cdbf9e).**
- [x] Deleting an env var needs no confirmation (x/backspace). Require a second press. **Done (6cdbf9e).**

**Other UI:**
- [x] `langwatch-36` (a `claude -p` stream-json process) is classified Interactive and told "reply there" (`fleet.go` interactive-session branch). **Done (1f8b726).**
- [x] Images: handle `file://` URLs, and U+202F in macOS screenshot names (`unicode.IsSpace` treats it as a separator in `splitPaths`). **Done (bb7b34e, 4808806 (image paths anywhere in a paste or message)).**
- [x] The "↓ N more" pill covers the last visible row (can be the selected one). **Done (51cfb49).**
- [ ] `isOpen` re-renders the whole session on every toggle.
- [x] `onHostLines` applies the last replay stamp to later live events in the same batch. **Done (not a bug: the host stamps every 500ms of output, live included).**
- [x] The narrow "waiting on you" row shows the raw tool name "AskUserQuestion". **Done (3be49fe).**
- [x] Question card option descriptions truncate instead of wrapping. Fixed after the handover: descriptions wrap under each option, "recommended" is a chip, ↑↓/enter/space choose while the card has focus, and a last row offers "answer in your own words". esc no longer skips.

### 2. Features the user asked for, not built yet

- [x] **Slash commands in the Session's message box** (built: `/` opens a picker of the session's commands plus agtop's `/clear`, `/model`, `/effort`; ↑↓, tab completes, enter runs). The user can't run `/clear`, `/compact` and so on from agtop mode today.
  - Typing `/` should open a picker above the box. Use the same row style as the Settings rows, with the command in bold white and its description dim.
  - The data comes from `convo.Session.Commands`, filled by the host's `initialize` reply (72 commands on the user's setup); narrow it as they type, and tab completes.
  - Enter sends `/cmd args` as the message; headless Claude Code runs slash commands it supports in stream-json (check which ones: `/compact` should work).
  - `/clear` needs agtop to handle it: start a fresh conversation in the same folder. That means spawning a new host session and selecting it, while the old one stays in the list.
  - agtop's own commands (`/agtop`, `/width`, `/done`, `/rename`, …) should appear in the same picker, marked as agtop's.
  - Added 2026-09-24: Claude Code's interactive screens, done agtop's way as **sheets** (`ui/sheet.go`: `m.sheet` takes every key and draws over the screen; async work comes back as `sheetMsg`). `/fork [name]` (`forksheet.go`): name, how much it remembers (up to any turn), model, effort, permissions, same folder or a new worktree (`actions.NewWorktree`, `.claude/worktrees/<name>`), first message; the whole conversation in place is `--fork-session`, anything else copies the transcript cut at a turn (`convo.TurnStarts`, `claude.CopyTranscript`, plus checkpoints) and resumes the copy. `/plugins` (`pluginsheet.go`, via `claude plugin … --json`): Installed (on/off, update, remove, parts and always-on tokens), Discover (search, install), Marketplaces (add, update, remove); a hosted idle session gets `/reload-plugins` after changes. `/statusline` (`statuslinesheet.go`, `internal/statusline`): up to three lines of segments with a live preview; saving points settings.json's statusLine at `agtop statusline`, and a status line command of your own is kept as the "Your own line" segment. `/skills` (`skillsheet.go`), `/permissions` and `/hooks` (`rulesheet.go`) likewise. `/memory` opens the memory view, `/config` Settings › Claude. `/status`, `/context`, `/usage` (alias `/cost`) and `/stats` open one tabbed sheet (`infosheet.go`, `infotabs.go`; ←→ or tab switch): **Status** (session, model, account, Claude Code's version and MCP servers from the init event), **Context** (Claude Code's `get_context_usage` control request, detail "summary" so it costs nothing: the host asks after every turn and on the `context` op, host Proto 2, and replays the last answer as an `agtop_context` line; drawn as a grid with a legend, then skills, MCP servers, agents and messages by size; the overview view has the same as a stacked bar), **Usage** (plan limits as wide meters, this agent's cost and tokens, every account), **History** (Claude Code's own `stats-cache.json`, `claude.LoadStats`: by day, by hour, by model), **Settings** (each area with what it holds now; enter opens its sheet, MCP opens Claude Code's). Claude Code's commands are sorted in `sessionviews.go`: ones agtop already does run agtop's (`agtopCommands` + `agtopAliases`: /diff → changes view, /tasks → subagents, /plan, /copy [n], /rename, /cd, /add-dir, /stop, /background, /resume, /help, /version → Status); account and cloud ones (`claudeCloud`) are offered as `claude:<name>` and open a real Claude Code after a notice (`claudeSheet`); Claude Code's own terminal's and the odds and ends are `offCommands`: out of the picker, and a flash when typed (/loops too: Claude Code has it switched off). /goal, /advisor, /autocompact, /skill-doctor and /fast run headless, so they go to Claude as messages and their output comes back as a synthetic answer. /btw (`side_question`; `btwpanel.go`) is a floating panel over the Session's top right, not a sheet: the chat and message box stay usable while it answers, ctrl+b moves the keys between them (not in a memory file, where it's bold), follow-ups carry the thread as history, ctrl+s puts the last answer in the box, ctrl+f pulls the thread out into its own agent (/fork of the whole conversation, the thread as its first message), ctrl+d closes it (from the chat too, while it's tucked away, with the box empty); leaving a thread with nothing asked closes it; threads are kept per agent in `m.btws`, and one asked on a connection you've since left is marked lost and /export (`export_conversation`: copy, or save in the agent's folder; `/export <file>` saves straight away) go through the host's `ask` op, which passes any control request to Claude Code, waking it if asleep, and returns the answer as an `agtop_reply` line under the client's id (`askClaude`, `onReply`, `asksheets.go`). /subtask asks Claude to send a background subagent off with the task. /artifacts, /workflows and /daemon are claude: ones. /mcp still hands the terminal over directly. A `/word` that neither agtop nor the session's command list nor the commands on disk know (`askUnknown`, hosted sessions only) opens a small sheet with two choices (↑↓, enter): open Claude Code on it, or send it to Claude as a message after all; esc keeps it in the box. Session-bound `/rewind` is the rewind session's (`rewindsheet.go`).
  - Added 2026-09-24: agtop's own status lines. The top bar (top right of the window) and the agent header (the two lines at the top of a Session) are layouts like Claude Code's status line, kept in `bars.json` (`statusline.Bars`) and drawn by agtop from its own data (`ui/bars.go`: `topSegs`, `agentSegs`). `/statusline` (and `#statusline` from the list, opening on Top bar) has three tabs: Agent header, Top bar, Claude Code. Edits on the agtop tabs show live on the real header; esc drops them; enter saves, and touches settings.json only if the Claude Code tab changed. When space runs out, the segments last on a line go first, whole. Not configurable: clanker and the counts, an agent's name, state and connection, the tab row and its alerts (✗ failed).
  - Added 2026-09-24: `/rewind` (aliases `/checkpoint`, `/undo`; `ui/rewindsheet.go`) takes an agtop-mode agent back to before one of your messages, in place: same agent, same host. agtop cuts a copy of the transcript at that message (`convo.TurnStarts` + `claude.CopyTranscript`, checkpoints linked over with `Account.CopyCheckpoints`), and the host's `rewind` op switches to it, clears its replay and drops clients so they redraw. The path left is kept in `host.Config.Branches` and listed in the same sheet to go back down. Code stays as it is by default (you keep the fix, lose the context); `c` puts files back via Claude Code's own `rewind_files` control request, previewed with a dry run. For that, hosts now run Claude Code with `CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING=1`, so only turns from after that change have checkpoints. `n` brings back a note: before cutting, a throwaway `claude -p --resume --fork-session --no-session-persistence` (`headless.Recap`) writes what the dropped turns learned, and it's put in the box above your old message.

- [x] **Queue view** (built: the queue view lists queued messages; enter edits one in the box and saves it back in place, [ ] moves, shift+↑↓ merges up or down, ctrl+s sends now, ctrl+x drops, alt+h holds, alt+o switches one-message/separately; the host now sends the whole queue as one message by default). Was: edit in place, reorder (shift+↑↓), merge, drop, send now, **hold**. Needs a `hold` op in the host, plus queue item ids. The client already has `EditQueued/MoveQueued/MergeQueued/SendQueued/RemoveQueued`; nothing calls them yet.
- [x] **Tasks view** (built: Now / Next / Done). Was: the full list (now / next / done), including subagents' tasks. Data is in `convo.Session.Tasks` (TodoWrite, TaskCreate, TaskUpdate).
- [x] **Subagents view redesign** (rows done in `0a2a077`: running/done/stopped/failed from each run's own transcript plus task notifications, real duration, steps, tokens, cost, model, latest words; note: Claude Code sometimes logs a response's usage mid-stream, so output tokens can read low). Still to do: master–detail, the runs list with the selected run's conversation beside it on wide panes. Richer rows: status, type, task, model, steps, tokens, duration, first line of its result. **Done (0a2a077 rows, 8ac2753 master–detail, c106f4f alt+↑↓ switcher).**
- [ ] **Overview redesign** (the user: "most of it sucks"): a dashboard.
  - Stat tiles across the top: cost, time, turns, tool calls, context %.
  - A per-turn cost/time chart, and the model/effort timeline as a strip.
  - Tool bars scaled properly (currently all but the top one look empty) and full tool names (currently cut, e.g. "AskUserQuest›").
  - Cache as a meter, with only the unexpected cold starts listed.
- [ ] **Drop the "screen" view** from the strip. It just shows that the agent runs Claude Code's whole app. Replace it with a header chip, "Claude Code app · /agtop for agtop mode"; ctrl+f still opens it full screen.
- [x] **Long pastes as chips:** ≥4 lines becomes `▤ pasted N lines`, sent in full; backspace on an empty box removes it. **ctrl+g opens `$EDITOR`** on the last paste, or on the whole draft, via `tea.ExecProcess` on a temp file. **Done (aecd341).**
- [x] Change model and effort from the UI (the client's `SetModel`/`SetEffort` exist; nothing calls them). Maybe `/model` and `/effort` in the message box. **Done (74cb852: /model and /effort pickers).**
- [ ] Render markdown tables in answers (currently raw pipes).
- [ ] Wide screens: the conversation caps around column 124/190 while the header runs full width. Align the right edges.
- [ ] The ▀▄ half-block bands look heavy at 250 columns and odd without colour. Consider a one-row background change.
- [ ] Focus at 80–100 columns (Session full width, no Agents side to dim) is signalled only by the placeholder. Add a stronger marker.
- [ ] Zen, changes view and search were built after the review and **haven't been reviewed**.

- [ ] **Full diff viewer.** The user wants the **changes** view grown into a full diff viewer tab:
  - file list with review marks
  - net diff per file
  - per-turn attribution
  - jump from a hunk into the conversation
- [x] **Recent changes rail**: built after the handover (latest edits as diff blocks beside the Session on wide screens, resizable, `/rail`), then removed on 2026-09-24 at the user's request. The Session and Agents share the width again; the changes view has the edits.

### 3. Nice to have / later

- [x] A right-hand side panel at ≥164-column panes (tasks, queue, shells), from the design. **Done (e03f1e9: 'now' section at the top of the rail); gone with the rail on 2026-09-24.**
- [ ] A Changes view built from git with per-turn attribution for the working tree (currently only marks this session vs not).
- [x] Desktop notifications (OSC 9) and a title counter while agtop is unfocused. **Done (adbcece: title counter, no notification for the agent you're watching).**
- [ ] A compaction divider and a context meter in the conversation.

### 4. Added 2026-09-24 (live task list in the session mirrors this)

Done this round, beyond the ticks above: `/` lists skills and custom commands and works mid-message (83787ca); short `↻` reset times and coloured usage meters with a pace tick (99246ba, 703308d); Claude Code sessions get agtop's queue and images (d8bae26); `/agtop` from the Session box, waits for the turn, forks terminal sessions (505b36c); keys go into a Claude Code screen directly (6fa3b02); artifacts view (dac1a98); the conversation shows a Claude Code agent's live screen line, todos and status (3e29b2a); quiet divider with resize pointer (4808806); one-row contextual hints and a shorter keys sheet (1af5422).

Later the same night: thinking and live token line (04effe8); failed steps show just the error, heredocs fixed (e64243c); performance pass (15 commits, 2–4× cheaper frames, streaming within a frame); zen/changes/search review fixes (f54f443, b9bbce0, d5b2dae); conversation review fixes (9096425); markdown tables (afd5dd6); compaction divider (b33fd2a); overview dashboard (23e5146); diff viewer with hunk attribution, review marks and git diffs (8f4cc65); subagent master–detail and alt+↑↓ switcher (8ac2753, c106f4f); rail "now" section (e03f1e9); paste chips and ctrl+g (aecd341); /model and /effort pickers (74cb852); artifacts view (dac1a98); layout polish (4009597).

Still open:
- [x] `RenderInto` reuses the pane's buffer (36bd36a). (Streaming, mouse redraws and the triple JSON parse are fixed: c716c96, 76a1b3b, 6545a46.)
- [x] The live screen reader, checked against a real Claude Code screen (prompt between rules, status lines) and seen working live.
- [x] After `/agtop` on a terminal session the original goes to Done and the copy keeps its name (3d1b064). Also: agtop's own Claude processes no longer list as extra agents (823550d).

Decided 2026-09-24: keep the screen tab (the user debugs with it); external context compaction is dropped. Also done: queue for Claude Code agents sends within 15s while working (6c70e64), pinned turn heading and denser turns (b68410e), history shown after /agtop (d6b2674), Claude Code notices shown (31b4a71), quieter dock and image chips (f3c0d33).

## How to test and look at it safely

**Tests and checks**
- `go vet ./... && go test ./...` must pass. Use `-race` for `internal/host`, `internal/headless` and `internal/convo`.
- Real-CLI tests are opt-in and cost a few cents of Haiku each: `AGTOP_REAL_CLAUDE=1 go test -run 'TestReal' ./internal/headless ./internal/host ./internal/convo`.

**Rendering one frame**
- `go build -o /tmp/agtop-bin ./cmd/agtop && /tmp/agtop-bin --render 170x46 [tab|down|enter|?|text=…|ctrl+x]`
- Pipe it through `sed 's/\x1b\[[0-9;]*m//g'` to read it.
- `AGTOP_RENDER_SELECT=<name part>` selects an agent and opens its Session. It connects to the real host or transcript, read-only.
- Don't press enter with text in a render: it starts real sessions.

**Isolating state**
- `AGTOP_HOME=/tmp/x` isolates agtop's config and sessions.
- Put `{"dispatch":{"model":"haiku"}}` in `$AGTOP_HOME/config.json` for cheap real sessions.
- The user rejected one extra real-session spawn at one point, so avoid spawning real sessions unless needed.

**After changes:** the user runs `go install ./cmd/agtop` to pick them up. Several "missing feature" reports were stale builds.
