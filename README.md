<h1 align="center">agtop</h1>

<p align="center">
  One place for every coding agent you run, written in Go and built to be very fast.<br>
  It started as a replacement for Claude Code's own view, which takes about 330 MB to show you one session. Now it runs Codex, Copilot, local models on Ollama and the rest the same way.
</p>

<p align="center">
  <img src="docs/screenshots/session-code.webp" alt="agtop: a Session with a failed step opened, its script and error">
</p>

<table>
  <tr>
    <td width="50%" valign="top">
      <b>Every agent at a glance</b><br>
      What each one is doing, in words, with its cost, tokens, time, CPU and RAM. Claude Code, Codex, Copilot, Gemini, Kimi, OpenCode, Vibe, DeepSeek, GLM and Ollama, in one list.
    </td>
    <td width="50%" valign="top">
      <b>Sessions you can read</b><br>
      Highlighted code, commands in plain words, which part of a Bash chain is stuck, diffs, the files changed, the queue, tasks and subagents.
    </td>
  </tr>
  <tr>
    <td valign="top">
      <b>What needs you, first</b><br>
      Turns that died on an error or a limit, questions, then turns that finished. Overview shows what's happening across every repo, and what happened today.
    </td>
    <td valign="top">
      <b>Providers and profiles</b><br>
      Every account of every agent, with its limits. A profile says which agents a folder runs on, and what to do when one runs out, down to handing the conversation to the next agent.
    </td>
  </tr>
  <tr>
    <td valign="top">
      <b>Where the tokens go</b><br>
      What you spent it on, which savers would cut it, what each might have saved you, and an optional advisor that goes looking for more.
    </td>
    <td valign="top">
      <b>Light</b><br>
      About 40 MB with a session open. The native view uses about 330 MB, and an idle Claude Code is stopped a few seconds after its turn.
    </td>
  </tr>
</table>

## Install

```sh
go install github.com/0xdeafcafe/agtop/cmd/agtop@latest
```

This needs Go 1.27.1 or newer. It puts `agtop` in `$(go env GOPATH)/bin`, so make sure that folder is on your `PATH`. On Apple Silicon, check that `go env GOARCH` says `arm64`: an amd64 Go builds an x86_64 agtop that runs under Rosetta. From a checkout, `GOARCH=arm64 go build -o "$(go env GOPATH)/bin/agtop" ./cmd/agtop` builds it natively.

```sh
agtop            # open it
agtop on         # make `claude agents` open agtop (adds one line to ~/.zshrc)
agtop off        # give `claude agents` back to Claude Code
agtop menubar    # put agtop in the macOS menu bar (`agtop menubar off` takes it out)
agtop update     # install the newest agtop
```

agtop checks for a newer version now and then and says so at the foot of the list. `#update` installs it from inside, the same as `agtop update`; reopen agtop to use it.

Getting started sits at the foot of the list until you've tried the basics, ticking each off as you go. `#tips` brings it back, `#tips off` puts it away, and `?` is a guide to the keys.

## Agents

The list is every agent you have, with what it's doing right now: "running pnpm test", "reading view.go", "searching for PrettyModel". Working agents stand out; idle, stopped and done ones step back. Each row carries its provider's glyph and colour, so a Codex session doesn't pass for a Claude one.

- **Needs you** comes first: an agent whose turn died says why, with a ✗ ("session limit · resets 5am"), and one with a question waits there too. One that finished without asking waits in **Your turn**. `alt+g` tells either to go on. A turn that died on the network or a flaky API ("Connection dropped", "Response stalled mid-stream") is told to continue by itself once the API can be reached.
- **Cost, tokens and time** for each agent, estimated from its transcript at list prices (subagents included), and today's spend per account.
- **CPU and RAM** for everything an agent started, not just the agent itself.
- **Preview** (`tab`): the agent's own screen, live, or what it's doing, its last message and its process tree. Type to reply without opening it.
- **Open** (`enter`) connects to the running session through the daemon, like the native view. `ctrl+]` comes back.
- **Agents that run agents**: when a session runs `claude -p`, `codex exec` or another agent's program from its shell, the row reads as that agent, with what it was asked and its latest steps. It's listed with the session's subagents rather than as a stray row of its own.
- **Done** (`alt+d`) moves an agent out of the way and stops its process if it's idle. A message resumes it. Nothing is merged or deleted.
- **Cold cache warning**: sending to a session idle past its prompt cache's hour asks first, since it re-reads the whole context uncached.
- **Groups** (`ctrl+s`) by status, agent or your own (`ctrl+e`), and **split by project** on top (`ctrl+p`, on by default): inside each section, agents sit together under a line per repository with its branch, commits ahead or behind, uncommitted changes and worktree count; agents in a linked worktree sit under the repository it came from, headed by the worktree's own branch and changes. The top of the list shows both and a click changes either. Pins (`ctrl+t`) are shared with the native view.
- **Change repo** (`ctrl+l`) moves a conversation to another folder or worktree.
- **Drafts**: `alt+s` keeps what you've typed and clears the box for the next thing; `alt+p` brings the latest back, and again for older ones. `#drafts` has them, with what you sent and what you cleared.

## Providers

agtop runs any agent it has an adapter for, and shows only the ones you have installed. It finds them on your `PATH` and where their installers put them, so an agent installed while agtop is open turns up without a restart.

| Provider | How agtop runs it | Support |
| --- | --- | --- |
| Claude Code | headless, hosted by agtop | full |
| Codex | its own `codex app-server` | tested |
| Copilot | the Copilot CLI, signed in with `gh`'s token; its coding agent's sessions on GitHub too, with their repo and PR | tested |
| Gemini, Kimi, OpenCode, Vibe | over the Agent Client Protocol | preview |
| DeepSeek | `dsh`, DeepSeek's own agent | preview |
| GLM | ZCode, Z.ai's own agent, through `zcode-acp-server` | preview |
| Ollama | Claude Code on Ollama's Anthropic API, tuned per model | preview |

Full is what I use every day, tested has been tried against the real program, and preview is built but not yet tried against it. Settings › Providers shows each one's level and what agtop can do with it, feature by feature: ✓ it can, – it can't, ◌ planned.

`#with codex` (or any of them) makes new sessions start with that agent; `#with claude` goes back, and `#with` alone says which it is and what's installed. Their past sessions sit in the list beside Claude's, marked with their agent, and a message resumes one with its own agent, model and mode. A Codex running in a terminal is followed as it works, as a Claude Code one is. In a session, `/` offers only what that agent can do.

**Ollama** runs Claude Code in a folder of its own, so a local model doesn't read your plugins, skills and MCP servers before every answer. Each session is fitted to its model: loaded before the first turn, told the context window it was loaded with so it compacts in time, one model for everything (titles, subagents, summaries) so nothing reaches Anthropic, and a prompt cut down to the six file and shell tools. On an M1 Max with `qwen3-vl:30b` that took the prompt from 14k tokens to 4k, and the first answer from over a minute to about seven seconds. With no model named it picks one already in memory, then one made for code, and refuses one that can't call tools.

### Hand-off

`/handoff codex` (or any agent that can take one on) starts that agent in a new session, in the same folder, opened with the conversation so far: how it started, what it did, the files it changed, where it left off and what's still to do. The session handed on stays as it is.

### Accounts

Settings › Providers lists each installed provider and, under it, the accounts it can run on: who each is, its plan, its limits, and which is in use. Every agent runs from its own home (`~/.claude`, `~/.codex`, …); an account is a sign-in swapped into it, so settings, transcripts and history are shared.

- `enter` switches to an account, `p` makes a provider the default. `a` adds an account, `r` renames, `l` signs in again, `d` forgets.
- `J` and `K` set the order of providers; `1`–`9` jump to one.
- Codex keeps several sign-ins in agtop's vault and swaps one into `~/.codex`. Copilot runs on whichever of `gh`'s GitHub accounts you pick, without changing `gh`'s own. DeepSeek shows its balance; GLM its Coding Plan's limits.

The top bar shows which provider, account and profile new sessions start on, that account's limits, and the battery and free disk. Settings › Agents shows one installed agent at a time (`1`–`9` picks it) and sets the model, effort and permissions its new sessions start with, plus what's that agent's own (for Claude Code: agent definitions, settings.json and its environment). Settings › Overview shows every provider's account, limits, spend today and this week, and what needs you. Settings › Profiles makes and edits profiles: which agents new sessions run, in order, what happens when their accounts run low, and which folders get which profile.

<table>
  <tr>
    <td width="50%" valign="top">
      <img src="docs/screenshots/accounts.webp" alt="Accounts, with each sign-in's usage"><br>
      <b>Accounts</b>
    </td>
    <td width="50%" valign="top">
      <img src="docs/screenshots/coding-agents.webp" alt="Coding agents"><br>
      <b>Coding agents</b>
    </td>
  </tr>
</table>

### Profiles

A profile is a named list of providers plus what to do when they run out:

- **Stay or mix**: new sessions stay on the first provider, or move on to the next once every account of the current one is nearly out.
- **At a limit**, for a conversation a usage limit stops: `wait` for the reset, move to another `account` of the same provider and carry on, or `handoff`, which tries another account first and then hands the conversation to the next provider in the list with room.

A session gets the profile picked for it (`#profile <name>`, or `agtop session start --profile`), else the one for the longest folder rule its folder falls under, else the default. It keeps that profile when it's resumed. Your old default agent and switching order became a profile called Default the first time agtop loaded your config.

## Sessions

An agent run in agtop mode (headless, hosted by agtop) opens in a Session beside the list. `[` and `]` move between its views: Conversation, Overview and Changes always, then Tasks, Queue, Subagents, Background and Artifacts when there's something in them, and Memory.

<table>
  <tr>
    <td width="50%" valign="top">
      <img src="docs/screenshots/session.webp" alt="The conversation"><br>
      <b>Conversation</b>. Clean steps fold to one row; a failed one shows the line that says what went wrong. Shell commands say what they do, and code is highlighted. Wide code and output wraps rather than being cut off.
    </td>
    <td width="50%" valign="top">
      <img src="docs/screenshots/session-code.webp" alt="A failed step opened, its script and error"><br>
      <b>A step, opened</b>. The script it ran and the error it hit. Drag to select and copy; <code>alt+c</code> copies Claude's answer as written.
    </td>
  </tr>
  <tr>
    <td valign="top">
      <img src="docs/screenshots/overview.webp" alt="The overview"><br>
      <b>Overview</b>. Spend, tokens and requests, cost per turn in each model's colour, where the model and effort changed.
    </td>
    <td valign="top">
      <img src="docs/screenshots/changes.webp" alt="Changes, with a new file's diff"><br>
      <b>Changes</b>. Every file the session changed, with its diff, which turn made each hunk and your review marks, and the rest of the working tree beside it.
    </td>
  </tr>
  <tr>
    <td valign="top">
      <img src="docs/screenshots/subagents.webp" alt="The subagents view"><br>
      <b>Subagents</b>. Every run, with its steps, tokens, cost and last words, working for as long as it is, and the selected run's conversation beside it. <code>enter</code> watches one.
    </td>
    <td valign="top">
      <b>Queue</b>. Messages sent while the agent works wait here. <code>enter</code> edits one, <code>shift+↑↓</code> merges it into the one above or below, <code>[</code> <code>]</code> move it, <code>s</code> sends it now, <code>ctrl+s</code> sends everything. Several go as one message, each numbered, so the agent reads them as separate requests.<br><br>
      Claude's questions arrive as one form, with a preview beside each option. Long pastes stay a chip; <code>ctrl+g</code> opens one in <code>$EDITOR</code>.
    </td>
  </tr>
</table>

Running shells, monitors, workflows and subagents sit in the dock under the conversation, each with its latest line: `↑` picks one, `b` sends it to the background, `x` stops it. A running Bash chain says which of its commands runs now and how long each finished one took ("3/4 go test"), and `k` ends the one it's stuck on and lets the chain carry on as it would after a failure. A tool call to allow, a question or a usage limit opens over the conversation and takes the keys; `esc` sets it aside in the dock.

`/` in the message box has the agent's commands and skills, and agtop's own sheets for the ones Claude Code draws itself:

- `/fork` a conversation, from any turn, into the same folder or a new worktree, with its own model, effort and permissions.
- `/rewind` to before one of your messages, keeping the code or putting the files back, with a note of what the dropped turns learned.
- `/btw` asks a side question in a panel while the agent keeps working.
- `/context`, `/status`, `/usage` and `/stats` open as one sheet: what fills the context, the limits, and your history by day, hour and model.
- `/plugins`, `/skills`, `/permissions`, `/hooks` and `/statusline`, and `/model` and `/effort` pickers.

When a turn ends and nothing is left running, Claude Code is stopped a few seconds later instead of sitting on 150–200 MB for five minutes doing nothing. The next message starts it again in about a second, with the prompt cache intact.

## Overview

The place next to Agents says what's happening across every repo, in three pages `[` `]` moves between: Now, Projects and the Wall.

**Now** is each working session: what it's doing, how far through its tasks it is, how full its context is, the heavy work running in its checkout (installs, type checks, tests, dev servers), and under it every subagent with the last thing it said, or the report it handed back.

**What happened** is the last day, newest first: what you asked, tasks planned and ticked off, subagents started and finished with their reports, background commands ending, turns ending with what they said, questions, commits, PRs, API errors and compactions. `f` keeps only the picked session's history; `enter` opens it in Agents.

**Projects** is each repository an agent worked in over the last day, whole: what its agents are doing, where it is and where it pushes, its branch with commits ahead and behind and uncommitted changes, its last three commits, the pull requests its agents opened with their checks and review, and every linked worktree with its own branch and changes, each with the agents working in it (or none). Folders that aren't repositories come last. `enter` opens an agent, `ctrl+y` its pull request.

**Wall** is every agent at once, streaming.

## Go anywhere

`ctrl+k` opens the command bar: a place, an agent, a Session's view, a turn (`#12`), `Back` to where you jumped from, or a new agent with what you typed. Words search the open conversation and every agent's transcript, and `in:name`, `is:failed`, `file:x` and `turn:10-13` narrow it. `ctrl+f` is the same bar, starting where you are. With many long transcripts, Settings › Interface › *ctrl+k searches transcripts* › *on ctrl+enter* keeps typing to names and commands; `ctrl+enter` then searches the transcripts (`ctrl+j` in terminals that send `ctrl+enter` as `enter`).

`#` runs agtop's own commands on the selected agent: `#done` `#go` `#stop` `#restart` `#rm` `#kill` `#clean` `#cd` `#add-dir` `#rename` `#group` `#pin` `#pr` `#full` `#sort` `#by` `#split` `#folder` `#with` `#profile` `#account` `#efficiency` `#advisor` `#statusline` `#view` `#width` `#dock` `#drafts` `#hibernate` `#native` `#mackeys` `#tips` `#update`. `/` is left to the agent.

<table>
  <tr>
    <td width="50%" valign="top">
      <img src="docs/screenshots/command-bar.webp" alt="The command bar"><br>
      <b>The command bar</b>
    </td>
    <td width="50%" valign="top">
      <img src="docs/screenshots/command-bar-search.webp" alt="The command bar searching agents and transcripts"><br>
      <b>Searching every transcript</b>
    </td>
  </tr>
</table>

## Efficiency

Where your tokens go, and whether the things that promise to cut them do. It reads every transcript (a few seconds the first time, only what's new after that) and keeps totals of the ones Claude Code deletes after 30 days.

- **Overview**. What was spent, the cache-hit rate, context per request, the context sessions start with, and where the money goes: usually context read again on every request, rarely what Claude writes. It names the savers that might save you most.
- **Timeline**. Any figure over the last day to 90 days (context per request, cost, tokens, output, cache hits, tool output per call, context at the start), with a marker wherever a saver was set up, a setting changed, a saver was first used, or you wrote a note (`n`). `b` compares the days either side of an event and, for a saver, sessions that used it against those that didn't in the same days.
- **Savers**. rtk, caveman, Ponytail, context-mode, headroom, Serena, Graphify, tokensave, codegraph, codebase-memory-mcp, claude-context, the language-server plugins, Context7, claude-mem, and Claude Code's own settings (shell output cap, compacting sooner, Sonnet for subagents, the Concise style, leaving out git instructions). For each: what it does, what its authors claim and what others measured, whether it's on here, and how often your sessions used it. `enter` shows exactly what setting one up would run and change before anything is done; files it may touch are backed up first, and `x` removes it.
- **Estimates**. What each saver might have saved you over the range: its measured cut of what your own sessions spent on the part it acts on (shell output, searching and reading code, what Claude writes, subagents on Opus…). The cut comes from independent tests where there are any, and says whose it is when there aren't; claims per question against reading every file aren't taken at their word. A saver in use is compared on sessions that used it against those that didn't, with a warning when there are too few to say much. `e` orders the Savers by estimate.
- **Findings**. What's worth doing, most dollars at stake first, each with the saver that addresses it.

`#efficiency` (or `#eff`) opens it.

### The advisor

Off until you turn it on with `#advisor on`. After your agents have done some work, at most every three hours, agtop works out a digest of the last week (spend, context, tool output, rereads, subagents, savers, the costliest sessions) and Haiku proposes up to five changes from it, reading short briefs of the transcripts for evidence. Opus then checks each one worth $2 a week or more against the transcripts, three a day at most, and rewrites or rejects it. Both run with no shell, web, hooks or MCP servers, and can't read outside those folders.

What Opus confirms goes first in the findings, marked ✦; `x` puts one away for good. A pass costs about $0.30, and the findings page shows what the advisor has cost so far. Only one agtop runs it at a time, and it keeps to its budget across windows and restarts.

## Machine

<table>
  <tr>
    <td width="50%" valign="top">
      <img src="docs/screenshots/processes.webp" alt="Processes"><br>
      <b>Processes</b>. Every agent's process tree, busiest first. What a finished session left running (a dev server, a watcher) is listed first: <code>x</code> ends one, <code>X</code> all of them.
    </td>
    <td width="50%" valign="top">
      <img src="docs/screenshots/cleanup.webp" alt="Cleanup"><br>
      <b>Cleanup</b>. Every worktree with its size and whether removing it would lose anything, and every agent's temp work. <code>A</code> removes everything that loses nothing.
    </td>
  </tr>
</table>

## Plugins

Plugins add tools, subagents, prompt text and memory to every agtop-mode session, and can run agents of their own. Any MCP server can be one. Each runs sandboxed and does only what you approved: no files but its own, no network but the hosts it named, no programs, and only the agents it started. macOS only for now.

```sh
agtop plugin list            # what's installed, approved and running
agtop plugin approve <name>  # read what it may do, and say yes
```

To have Claude write one, install the skill: `/plugin marketplace add 0xdeafcafe/agtop`, then `/plugin install agtop-plugin-dev@agtop`.

A plugin can also take part in agtop's screen, as far as you approved:

- hear what happens there (sessions opened, turns ending, a session an error stopped, the network going and coming back);
- add sections to a Session's overview and a word to its row;
- add commands you can bind to keys, and settings under **Settings → Plugins**;
- with `input`, see and set what you type;
- with `intercept`, change or hold back a message before it goes.

None of it can hold agtop up. The screen hands plugins events without waiting, draws what they added from a copy it already has, and gives an intercept 400 ms before the message goes as it was. The [`autodrafts`](plugins/examples/autodrafts) and [`reconnect`](plugins/examples/reconnect) examples rebuild drafts and reconnect-and-continue this way.

[plugins/](plugins) has everything else: using and writing them, the examples, the skill, and how it works.

## Embedding agtop

Another app can run agtop-mode sessions without the view and show one of them in a terminal of its own.

```sh
agtop session start --cwd DIR [--agent A] [--profile P] [--session-id UUID] [--resume] [--name N] \
  [--prompt-file F] [--image PATH]... [--env K=V]... [--meta k=v]... \
  [--binary PATH] [--model M] [--effort E] [--permission-mode M] --json
echo 'the next message' | agtop session send <id> [--now] [--image PATH]...
agtop session interrupt <id>
agtop session stop <id>
agtop session info <id> --json
agtop session list --json [--meta k=v]...
```

`start` runs the first installed provider of the session's profile (`--profile`, else the folder's rule, else the default) unless `--agent` names one (`codex`, `copilot`, `kimi`…). It uses the model, effort, permission mode and limit settings from Settings unless a flag gives them (for another agent, its own), and prints the session's info with `"alive"` added. With `--session-id` it is idempotent: a session already running is printed, not started again. A stopped one needs `--resume`, which brings the same conversation back. `--env` values reach the agent on every start of it, idle restarts and resumes included. `--meta` tags the session; `list --meta` filters on the tags.

`send` reads the message from stdin. If the session is stopped it resumes with the message, as sending from the view does. `info` exits 1 with `{"error":"not found"}` for an id with no session. `alive` is whether the session's host is running; a host whose agent is resting while idle counts as alive.

A plugin can also arrange the Agents list for an embedding app: with the `sidebar` capability it sends sections and a name for each agent, keyed by session id, and the list offers them as a group-by mode (`ctrl+s`, or `/by plugin:<name>`). The [`kanban`](plugins/examples/kanban) example shows the kanban-code board this way.

`agtop open <id> --solo` is the view of that one session alone, under agtop's header, with the Session at the terminal's whole width and no Agents list. `ctrl+\` opens Efficiency, Machine and Settings as usual, and Agents is the session again. The keys that lead to other agents or open the list (`ctrl+z ctrl+n ctrl+k tab`) do nothing, and `, . < >` are typed into the box. The message box has the keys from the start. `esc` at the top level and `ctrl+q` close the view; the session keeps running. A stopped session shows its conversation and resumes with the first message.

## And

- **Menu bar**: every account's limits, each by its own name (Codex's week reads 7d), the agents working, and a badge for each waiting on you. Questions arrive as notifications you can answer from; clicking one brings back the terminal agtop is open in (Warp, iTerm, Ghostty…) on that agent. agtop offers it the first time it opens on a Mac. It's a small Swift app built on your Mac the first time (it needs Xcode's command line tools).
- **Zen** (`ctrl+z`): only the agent that needs you and its box, then the next one. A bar across the top says where you are in the queue; `ctrl+n` skips, holding `tab` peeks at what's working, `ctrl+z` again leaves.
- **Terminal.app** keeps ⌘ for its own menus. `#mackeys on` sets up Hammerspoon to send ⌘← → ⌘⌫ ⌘⌦ and ⌘Z on as editing keys, only while Terminal.app is in front; `#mackeys off` takes it out again.
- **Status lines**: `/statusline` lays out the agent header, agtop's top bar and Claude Code's own status line, with a live preview.
- **Colours** made from your terminal's own background and text, or set to dark or light, and a colour-blind palette, in Settings › Interface.
- **Light**: about 40 MB with a session open, 56 MB for a 38-hour session with 342 subagent runs. Idle, it does almost nothing: the kernel says when a transcript changed, and only that file is read again. `agtop --soak 30s 200x50` measures it against your own agents.

## Keys

| Key | |
| --- | --- |
| `↑` `↓` `⌘↓` | move, open the agent (or `→`) |
| `enter` | rename or open it, as you chose in Settings |
| type, `enter` | start a session, or reply to the agent in the preview |
| `tab` | the list ⇄ the agent's Session |
| `ctrl+k` | go anywhere, search everything |
| `ctrl+f` | find, starting where you are |
| `<` `>` · `ctrl+\` | Agents · Overview · Efficiency · Machine · Settings; in a Session's box `,` `.` `<` `>` are typed, so `ctrl+\` |
| `[` `]` | the next page, everywhere there are pages: a Session's views (with nothing typed), Efficiency, Machine, Settings, and the tabs of a sheet |
| `ctrl+r` `ctrl+t` `ctrl+e` | rename, pin, set group |
| `ctrl+s` | group by status, agent, your groups |
| `ctrl+p` | split each section by project, or not |
| `ctrl+n` | the next agent needing you |
| `alt+g` | tell it to go on |
| `alt+s` `alt+p` | keep a draft, bring one back |
| `alt+d` | done |
| `ctrl+x` | stop; twice on a stopped agent deletes it |
| `ctrl+l` | change repo |
| `ctrl+z` | Zen |
| `#` | agtop's commands |
| `/` | the agent's commands and skills |
| `?` | the guide |

Every key can move. **Settings → Keys** lists every action, where it works (everywhere, the list, a Session) and its keys. Press `enter` on one, then the keys you want. A chord is several, `ctrl+x` then `d` say, ended with `enter`. `a` adds a key, `x` leaves it with none, and `r` puts agtop's back. Each `#` command, and each plugin's commands, can have keys too. A key that would take another's asks first. What you change goes in `~/.config/agtop/keybindings.json`:

```json
{"bindings": {"session.send": ["ctrl+enter"], "command:drafts": ["ctrl+x d"], "list.pr": []}}
```

## What's coming

None of this is in agtop yet. Some of it is being built now, and the rest is next in line.

- **A Profiles page**: create and rename profiles, order their providers, set what they do at a limit, and add a folder rule from the selected session's folder. A key to switch the default profile or provider from anywhere, with a picker.
- **Limits and prices for everyone**: limits for Kimi and Vibe, prices for Codex and the ACP agents (Copilot's models already say their premium-request multiplier), and Efficiency for agents other than Claude Code.
- **Copilot's coding agent**, started and steered from agtop rather than only watched.
- **Codex accounts** read live when they aren't the one in use, rather than showing their last reading.
- **Ollama sessions** in the list with the rest, and re-tuned when you change model mid-session.
- **Plugins' tools in every agent**, over MCP, which the ACP agents already accept.
- **ZCode spoken to directly**, dropping the `zcode-acp-server` bridge, and Goose over ACP.
- **Claude Code as just another adapter**: its discovery moves into its adapter, and its sessions go over the wire as agtop's own events like everyone else's. You won't see this one, but it's what makes the rest cheap to add.
- **`agtop on` for other agents**, if `codex` or `copilot` grow a view worth replacing.

Rewind, fork, checkpoints, the context breakdown, background task control and the screen tab have no equivalent in the Agent Client Protocol, so they stay hidden for those agents rather than faked. [docs/multi-agent.md](docs/multi-agent.md) has the design and where it stands.

## How it works

agtop reads Claude Code's files: `jobs/*/state.json`, `daemon/roster.json`, `jobs/pins.json`, the transcripts and the cached plan usage. It changes things only through Claude Code (the daemon's control socket, or the `claude` CLI), with one exception: switching account writes the other sign-in into Claude Code's keychain item and its `oauthAccount` into `~/.claude.json`. Each sign-in is kept in your login keychain as `agtop-login`.

Other agents run through adapters in [internal/adapters](internal/adapters): Codex over its app-server's JSON-RPC, the rest over the Agent Client Protocol, and Ollama through Claude Code. Each adapter declares which of agtop's features it supports, and the core asks that rather than checking for an agent by name. Whatever the agent, its events draw as a session. Switching a Codex account puts its sign-in in `~/.codex`, keeping the one there first so its refreshed tokens aren't lost; an API-key sign-in is named by a hash of the key, never the key.

Its own state (Done, names, groups, accounts and profiles, not their sign-ins) lives in `~/.config/agtop`, and a cost cache in `~/Library/Caches/agtop`.

Plugins run under `agtop plugind`, sandboxed; [plugins/ARCHITECTURE.md](plugins/ARCHITECTURE.md) has how.

The control socket and the files are undocumented, checked against Claude Code v2.1.280. If an update changes them, the column affected shows `–`, and opening an agent falls back to `claude attach`.

Limits are asked of each agent at most every five minutes per account, shared by every agtop that's open. Costs are estimates at list prices, not your bill.
