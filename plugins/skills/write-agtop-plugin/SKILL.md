---
name: write-agtop-plugin
description: Use when the user wants to write, scaffold, debug or package an agtop plugin — a sandboxed program that gives Claude tools in agtop-mode sessions, adds subagents or system-prompt text, keeps memory, or lists, starts, follows and messages agents through agtop. Triggers on "agtop plugin", "write a plugin for agtop", "plugin.json for agtop", "make this MCP server an agtop plugin", "agtop plugin approve fails", "my agtop plugin won't start", or questions about agtop's plugin protocol, fd 3, the plugin broker (plugind) or its sandbox. Not for Claude Code plugins (skills, hooks, marketplaces): those are a different thing.
---

# Writing an agtop plugin

An agtop plugin is a program agtop runs **sandboxed** next to the user's agents. It can:

- offer **tools** Claude calls in every agtop-mode session, as `mcp__agtop-<plugin>__<tool>`;
- add **subagents** (`<plugin>:<agent>`) and **system-prompt text** to every session;
- keep **memory** in its data folder;
- **list, start, follow, message and stop agents** through agtop.

It is not a Claude Code plugin. It lives in `~/.config/agtop/plugins/<name>/` (or `$AGTOP_HOME/plugins/<name>/`), is described by a `plugin.json`, and runs only after the user approves it with `agtop plugin approve <name>`. Plugins run on macOS only; elsewhere agtop refuses to run them.

Read the reference that fits the task before writing code:

- `references/protocol.md` — the wire format and every method, both ways, with examples.
- `references/manifest.md` — every `plugin.json` field and its limits.
- `references/sandbox.md` — what the sandbox allows, and what each failure looks like and how to fix it.

Working templates, each tested in the real sandbox: `templates/go`, `templates/python`, `templates/node`. `scripts/call.py` calls a running plugin's tools the way a session would.

## 1. Choose the shape

| The user wants | Build |
|---|---|
| Tools that already exist as an MCP server (memory, a database, an API) | an **MCP plugin**: `"protocol": "mcp"`, the server run unchanged. It can't call agtop back. |
| Tools of their own, or anything touching agents (list, start, follow, message) | an **agtop plugin** (the default protocol): start from a template. |
| Only prompt text or subagents | Say that a Claude Code plugin or CLAUDE.md may suit better: an agtop plugin always runs a program. If they want it in agtop anyway, use a template with its tools removed and `"tools": false`. |

Language: Go gives a single static binary that needs nothing else in the sandbox, the most robust choice. Python and Node work through Homebrew's interpreters (see `references/sandbox.md` for others).

## 2. Ask for as little as possible

Every capability is shown to the user at approval. List only the ones the plugin uses:

| Need | Manifest |
|---|---|
| Tools for Claude | `"tools": true` |
| See which agents exist, their state and cost | `"sessions": ["list"]` |
| Start agents | `"start"` and `"workspaces": ["~/Source"]`, the folders they may run in |
| Follow what its agents say | `"read"` |
| Message its agents | `"send"` |
| Interrupt or stop its agents | `"control"` |
| Reach the internet | `"network": ["api.example.com:443"]`, exact hosts and ports, HTTPS only |
| Read files outside its folder | `"read": ["/abs/path"]`, only for an interpreter's libraries |

`read`, `send` and `control` reach **only agents the plugin started**. Don't design around reaching the user's other agents: agtop refuses, by design.

## 3. Scaffold

1. Copy the template for the language into a working folder and rename `notes` to the plugin's name everywhere, including `"name"` in `plugin.json`. The name is lowercase letters, digits and dashes, and must match the folder. Keep the plugin name plus the longest tool name under 51 characters (Claude's 64-character tool-name limit, less `mcp__agtop-` and `__`).
2. Replace the example tools. Keep everything under `--- plumbing: keep as is ---`.
3. Write tool descriptions for Claude: say when to use the tool, not only what it does. Keep results short and textual; errors go back as `isError: true` results Claude can read, not as protocol errors.
4. Store state under `$AGTOP_PLUGIN_DATA`, which is also `$HOME` and `$TMPDIR`'s parent. It's the only writable place, and it survives updates and re-approvals.

For an MCP plugin, write only `plugin.json`: see the memory example in `references/manifest.md`, and install the server's files (`npm install …`) into the plugin folder itself.

## 4. Install and check

```sh
mkdir -p ~/.config/agtop/plugins/<name>
# Go:     go build -o ~/.config/agtop/plugins/<name>/<name> .
# Others: copy the script files
cp plugin.json ~/.config/agtop/plugins/<name>/
agtop plugin check <name>
```

`agtop plugin check` validates the manifest, confirms the program exists, and prints exactly what approval would allow. Fix everything it reports. Read its output back to the user: that's what they'll be agreeing to.

## 5. Approval is the user's, not yours

The user runs `agtop plugin approve <name>` **themselves, in a terminal**. It needs a real TTY on purpose. Never try to approve a plugin for them: don't fake a terminal, script the prompt, or edit `approved.json`. Tell them what the approval will show, and why the plugin needs each thing.

Any change to any file in the plugin's folder stops it until it is approved again. Every rebuild means another `agtop plugin approve <name>`, so batch changes before asking.

## 6. Test

```sh
python3 <this skill>/scripts/call.py status                     # is it running?
python3 <this skill>/scripts/call.py <name> list                # its tools
python3 <this skill>/scripts/call.py <name> call <tool> '{"arg": "value"}'
agtop plugin logs <name>                                        # where stderr (and an agtop plugin's stdout) go
```

Then try it in a real agtop-mode session: new sessions get the plugin's tools, subagents and prompt text. Running ones get them when Claude Code next starts. Each tool call asks the user for permission, like any tool.

If it won't start or a call fails, read `references/sandbox.md` § Troubleshooting before changing code. Most failures are the sandbox doing its job, and the fix is in the manifest or in where the plugin writes.

## Rules the plugin must keep

- Talk only on fd 3 (agtop plugins) or stdin/stdout (MCP plugins). An MCP plugin must never print anything else to stdout.
- Answer `initialize` within 15 seconds, and handle requests concurrently: a slow tool must not block `tools.list`.
- Exit when fd 3 (or stdin) closes.
- Start no subprocesses, and write nowhere but `$AGTOP_PLUGIN_DATA`.
- Stay under `memoryMB` (default 256). The broker kills a plugin over it and restarts it, waiting longer each time.
- Expect restarts at any time: keep state on disk, not only in memory. A restart also ends its `sessions.subscribe` follows, so subscribe again when needed.
