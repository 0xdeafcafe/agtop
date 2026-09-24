# Plugins

A plugin adds to every agtop-mode session: tools Claude can call, subagents, text in the system prompt, a memory it keeps across sessions. It can also run agents of its own: start them, follow what they say, message them. Any MCP server can be one, unchanged.

Every plugin runs sandboxed and does only what you approved. It can't read your files, reach the internet (except hosts it named and you approved), start programs, or take over your agents.

Plugins run on macOS only for now. Elsewhere agtop won't run them rather than run them unsandboxed.

| | |
|---|---|
| [Using plugins](#using-plugins) | install, approve, list, revoke |
| [What a plugin can't do](#what-a-plugin-cant-do) | the sandbox, in short |
| [Writing one](#writing-one) | with Claude and the skill, or by hand |
| [Examples](examples) | `neighbours`, `kanban`, `delegate` and `memory` |
| [ARCHITECTURE.md](ARCHITECTURE.md) | the processes, the boundaries, why it's built this way |
| [Protocol](skills/write-agtop-plugin/references/protocol.md) · [Manifest](skills/write-agtop-plugin/references/manifest.md) · [Sandbox](skills/write-agtop-plugin/references/sandbox.md) | the reference |

## Using plugins

A plugin is a folder in `~/.config/agtop/plugins/<name>` holding a `plugin.json` and its program. Nothing runs until you approve it:

```sh
agtop plugin list            # what's installed, approved and running
agtop plugin check <name>    # is it valid, and what would approving it allow
agtop plugin approve <name>  # read what it may do, and say yes
agtop plugin revoke <name>   # stop it
agtop plugin logs <name>     # where its output goes
```

Approving shows, in plain words, what the plugin can read, write and reach, and what it adds to your sessions. It also records every file in the folder: if one changes, the plugin stops until you approve it again. The prompt text, subagents and tools your sessions get come from the manifest as you approved it, not as it is on disk now.

New sessions pick up approved plugins. A running session picks them up the next time its Claude Code starts, which happens after it rests. Each of a plugin's tools asks you before it runs, like any tool, unless you allow it.

Plugins run under `agtop plugind`, one small process agtop starts once a plugin is approved and that ends when none is.

## What a plugin can't do

- **Your files.** It reads its own folder and the system's libraries, and writes only its data folder, `~/.config/agtop/plugin-data/<name>`, plus any other paths its manifest names under `read` and `write`.
- **The network.** It has none, unless its manifest names `host:port` pairs. Those it reaches through agtop's proxy, over HTTPS only, and never your machine or your network, whatever a name resolves to.
- **Programs.** It can't start any. It can ask agtop to run the ones its manifest names under `exec`, which run outside the sandbox, as you. Approval lists each.
- **Your environment.** It gets a clean one: none of your tokens or Claude Code settings.
- **Your agents.** It may list and watch them, and see their folder, branch, state, cost and context use, never what was said. It may message, follow and stop only agents it started. Those run in folders you approved (in a new worktree, if it asks), never in a mode that skips asking you, with at most 4 at once and 30 an hour. With `queue` it may also queue a message, marked as its own, to other agents in those folders that still ask you before acting.
- **Your machine's time.** It runs at utility QoS, and is ended if it grows past its memory limit (256 MB by default).

The full list is in [the sandbox reference](skills/write-agtop-plugin/references/sandbox.md), and how each rule is enforced is in [ARCHITECTURE.md](ARCHITECTURE.md#the-boundaries).

## Writing one

### With Claude

The [`write-agtop-plugin`](skills/write-agtop-plugin) skill teaches Claude to write, test and debug plugins. It knows the protocol, the manifest and the sandbox, starts from a template in Go, Python or Node that's tested in the real sandbox, checks its work with `agtop plugin check`, and tests the tools with its `call.py`. Approving stays with you.

```
/plugin marketplace add 0xdeafcafe/agtop
/plugin install agtop-plugin-dev@agtop
```

Then ask: *"write me an agtop plugin that …"*.

### By hand

1. Copy a template: [`go`](skills/write-agtop-plugin/templates/go) (a static binary, the sturdiest), [`python`](skills/write-agtop-plugin/templates/python) or [`node`](skills/write-agtop-plugin/templates/node). Each is one file with no dependencies, and offers three tools: two keep notes in the data folder, and one calls agtop back to list agents.
2. Rename it, change its tools, and keep the plumbing.
3. Put it in `~/.config/agtop/plugins/<name>/`, run `agtop plugin check <name>`, then `agtop plugin approve <name>`.
4. Call its tools without a session: `python3 skills/write-agtop-plugin/scripts/call.py <name> list`, then `… call <tool> '{"arg": "value"}'`.

In short, it talks to agtop on **fd 3**, one end of a socket pair agtop made for it. Each message is JSON-RPC 2.0 behind a 4-byte big-endian length. agtop calls `initialize`, `tools.list` and `tools.call`; the plugin can call `sessions.list`, `sessions.watch`, `sessions.start`, `sessions.send`, `sessions.queue`, `sessions.subscribe`, `sessions.stop` and `exec`, if its manifest asks for them. A manifest with `"protocol": "mcp"` makes it an ordinary MCP server on stdin and stdout instead.

```jsonc
{
  "name": "delegate",                 // lowercase, digits, dashes; the folder's name
  "command": ["delegate"],            // a path in the folder, or absolute (an interpreter)
  "tools": true,                      // offer its tools to sessions (always, for "mcp")
  "sessions": ["list", "start", "read", "send", "control"],
  "workspaces": ["~/Source"],         // where it may start agents
  "network": ["api.example.com:443"], // what it may reach
  "memoryMB": 64,
  "agents": { "reviewer": { "description": "…", "prompt": "…" } },  // as delegate:reviewer
  "prompt": "…"                       // added to every session's system prompt
}
```

Every method and event: [protocol](skills/write-agtop-plugin/references/protocol.md). Every field: [manifest](skills/write-agtop-plugin/references/manifest.md). When something won't start: [sandbox § troubleshooting](skills/write-agtop-plugin/references/sandbox.md#troubleshooting).

## Examples

- [`neighbours`](examples/neighbours) is the smallest useful plugin, and the one to read first: one tool that tells Claude which other agents are working in the same repository, and on which branch, so it doesn't trip over them. About 100 lines of Go, with only the `list` capability.

  ```sh
  mkdir -p ~/.config/agtop/plugins/neighbours
  go build -o ~/.config/agtop/plugins/neighbours/neighbours ./plugins/examples/neighbours
  cp plugins/examples/neighbours/plugin.json ~/.config/agtop/plugins/neighbours/
  agtop plugin approve neighbours
  ```

- [`kanban`](examples/kanban) connects agtop to [kanban-code](https://github.com/langwatch/kanban-code), the board that shows coding agents as cards. It shows how a plugin works with another tool on your machine: it reads the tool's files and drives its CLI through `exec`. With it:
  - Claude can read the board, and the card it's working on, with the issue, the PR, failing checks and unresolved review threads.
  - Claude can start an agent on a card, in the card's worktree or a new one, tagged with the card. The plugin then has kanban-code link the card to the agent's conversation (`kanban relink`), so the card follows it across the board.
  - When a card's PR fails a check or gets a new review thread, the agent working on it is sent a message when its turn ends.

  It needs kanban-code's app running for the link, and its CLI at `~/.local/bin/kanban`, where the app installs it. Change `workspaces` in `plugin.json` to where your projects are.

  ```sh
  mkdir -p ~/.config/agtop/plugins/kanban
  go build -o ~/.config/agtop/plugins/kanban/kanban ./plugins/examples/kanban
  cp plugins/examples/kanban/plugin.json ~/.config/agtop/plugins/kanban/
  agtop plugin approve kanban
  ```

- [`delegate`](examples/delegate) lets Claude hand work to agents of its own, follow them and message them. It's written in Go, uses every session capability, and brings a `reviewer` subagent.

  ```sh
  mkdir -p ~/.config/agtop/plugins/delegate
  go build -o ~/.config/agtop/plugins/delegate/delegate ./plugins/examples/delegate
  cp plugins/examples/delegate/plugin.json ~/.config/agtop/plugins/delegate/
  agtop plugin approve delegate
  ```

- [`memory`](examples/memory) is the reference MCP memory server, `@modelcontextprotocol/server-memory`, run unchanged: a knowledge graph Claude keeps across sessions, stored in the plugin's data folder.

  ```sh
  mkdir -p ~/.config/agtop/plugins/memory && cd ~/.config/agtop/plugins/memory
  npm install @modelcontextprotocol/server-memory
  cp <agtop>/plugins/examples/memory/plugin.json .
  agtop plugin approve memory
  ```
