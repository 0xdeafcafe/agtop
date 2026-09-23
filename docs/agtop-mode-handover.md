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
- The list (**Agents**) keeps ≥25% width (≥30 columns). There's no split when the pane would be under 84 columns. The user can resize (alt+←/→, drag the divider, `/width 30%`).
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
  - F2 renames
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
- [ ] An auto-continue limit can freeze the queue for good. `busy` stays true while `Limit.Continue` is set, but `sendLocked` stops the timer, a limit with no `ResetsAt` never schedules one, and the `limit` op can leave `Continue` set with no timer. Clear `Limit` on send and on a successful Result.
- [ ] The limit text match (`"usage limit"`, `"limit reached"`) runs before the `IsError` check, so a successful answer mentioning it counts as a limit. Only match when `IsError`. Reset `limitRaw` after a successful turn.
- [ ] Sends during an idle stop or effort stop are lost: `mu` is released while `s.sess` still points at the dying process. Set `s.sess = nil` under `mu` before calling `Stop`.
- [ ] A retry/continue timer can fire after the user has already sent (`AfterFunc` waiting on `mu`). Add a generation counter checked inside `after`.
- [ ] A double `stop` panics on `close(s.quit)`. Use `sync.Once`.
- [ ] Up to ~7 MB of image base64 is written to stdin while holding `mu` (risk of deadlock). Write outside the lock.
- [ ] State never returns to "working" when Claude starts another turn after a Result (e.g. background-task wakeups). Set working on `Status`/`MessageStart` events.
- [ ] Queue ops are addressed by index, which races with the automatic pop. Give queue items ids.
- [ ] A popped queue item is lost if its send fails. Re-queue it.
- [ ] The queue still drains after an interrupt. Hold on interrupt.
- [ ] An over-long line: the scanner's 64 MB buffer can make `Wait` hang. Surface the error and restart.
- [ ] **The whole queue should send as one message by default** (the user agreed). The host currently pops one item per turn.

**Convo** (`internal/convo`):
- [x] **Phantom live turn** (fixed after the handover: `turnFor(now)` sets `Start`, tool results go to their step's own turn, and a turn with no prompt reads "picked up on its own"). Was: `turnFor()` created a Live turn with zero `Start`, so the header shows "✻ working 2562047h" (seen on real sessions). Set `Start: now`, and route messages with a parent and tool_results to their step's own turn rather than opening a new one.
- [ ] Subagent output leaks into the main turn when its parent step is unknown (checks `parent != nil`, should check `ParentToolUseID != ""`). It also overwrites `Context` and the turn's model.
- [ ] **Totals are wrong:**
  - `request()` only merges a repeat of the *last* message id, so parallel subagents double count. Use a map by id.
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
- [ ] Values are HTML-escaped on save: `json.Marshal` of a `RawMessage` escapes `<>&`, so hook commands like `a && b > x` get mangled. Write with `SetEscapeHTML(false)`, or `json.Indent` the raw bytes.
- [ ] Keys get re-sorted. Keep the original order (decode with `json.Decoder` tokens).
- [ ] A symlinked settings.json (dotfiles) is replaced by a plain file. Save to `filepath.EvalSymlinks(path)`.
- [ ] Changes made in the meantime by Claude Code's `/model` or `/config` are overwritten. Re-read and apply only the changed keys just before saving.
- [ ] Deleting an env var needs no confirmation (x/backspace). Require a second press.

**Other UI:**
- [ ] `langwatch-36` (a `claude -p` stream-json process) is classified Interactive and told "reply there" (`fleet.go` interactive-session branch).
- [ ] Images: handle `file://` URLs, and U+202F in macOS screenshot names (`unicode.IsSpace` treats it as a separator in `splitPaths`).
- [ ] The "↓ N more" pill covers the last visible row (can be the selected one).
- [ ] `isOpen` re-renders the whole session on every toggle.
- [ ] `onHostLines` applies the last replay stamp to later live events in the same batch.
- [ ] The narrow "waiting on you" row shows the raw tool name "AskUserQuestion".
- [x] Question card option descriptions truncate instead of wrapping. Fixed after the handover: descriptions wrap under each option, "recommended" is a chip, ↑↓/enter/space choose while the card has focus, and a last row offers "answer in your own words". esc no longer skips.

### 2. Features the user asked for, not built yet

- [ ] **Slash commands in the Session's message box.** The user can't run `/clear`, `/compact` and so on from agtop mode today.
  - Typing `/` should open a picker above the box. Use the same row style as the Settings rows, with the command in bold white and its description dim.
  - The data comes from `convo.Session.Commands`, filled by the host's `initialize` reply (72 commands on the user's setup); narrow it as they type, and tab completes.
  - Enter sends `/cmd args` as the message; headless Claude Code runs slash commands it supports in stream-json (check which ones: `/compact` should work).
  - `/clear` needs agtop to handle it: start a fresh conversation in the same folder. That means spawning a new host session and selecting it, while the old one stays in the list.
  - agtop's own commands (`/agtop`, `/width`, `/done`, `/rename`, …) should appear in the same picker, marked as agtop's.

- [ ] **Queue view** in the strip: edit in place, reorder (shift+↑↓), merge, drop, send now, **hold**. Needs a `hold` op in the host, plus queue item ids. The client already has `EditQueued/MoveQueued/MergeQueued/SendQueued/RemoveQueued`; nothing calls them yet.
- [ ] **Tasks view:** the full list (now / next / done), including subagents' tasks. Data is in `convo.Session.Tasks` (TodoWrite, TaskCreate, TaskUpdate).
- [ ] **Subagents view redesign** (the user: "sucks"): master–detail, the runs list with the selected run's conversation beside it on wide panes. Richer rows: status, type, task, model, steps, tokens, duration, first line of its result.
- [ ] **Overview redesign** (the user: "most of it sucks"): a dashboard.
  - Stat tiles across the top: cost, time, turns, tool calls, context %.
  - A per-turn cost/time chart, and the model/effort timeline as a strip.
  - Tool bars scaled properly (currently all but the top one look empty) and full tool names (currently cut, e.g. "AskUserQuest›").
  - Cache as a meter, with only the unexpected cold starts listed.
- [ ] **Drop the "screen" view** from the strip. It just shows that the agent runs Claude Code's whole app. Replace it with a header chip, "Claude Code app · /agtop for agtop mode"; ctrl+f still opens it full screen.
- [ ] **Long pastes as chips:** ≥4 lines becomes `▤ pasted N lines`, sent in full; backspace on an empty box removes it. **ctrl+g opens `$EDITOR`** on the last paste, or on the whole draft, via `tea.ExecProcess` on a temp file.
- [ ] Change model and effort from the UI (the client's `SetModel`/`SetEffort` exist; nothing calls them). Maybe `/model` and `/effort` in the message box.
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
- [x] **Recent changes rail** (built after the handover). On wide screens, room past the Session's 128 columns becomes a rail of the latest edits as diff blocks. The Session never takes more than 128 columns; if the rail doesn't fit, the room goes to Agents. `layout()` sets `m.railW`; the rail comes from `convo.Session.RecentEdits`. The user's own list width wins, so a wide saved `/width` means no rail.

### 3. Nice to have / later

- [ ] A right-hand side panel at ≥164-column panes (tasks, queue, shells), from the design.
- [ ] A Changes view built from git with per-turn attribution for the working tree (currently only marks this session vs not).
- [ ] Desktop notifications (OSC 9) and a title counter while agtop is unfocused.
- [ ] A compaction divider and a context meter in the conversation.

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
