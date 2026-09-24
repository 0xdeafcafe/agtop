# plugin.json

The manifest lives at `<plugins>/<name>/plugin.json`. Unknown fields are an error, so a typo can't silently drop a restriction. Run `agtop plugin check <name>` after every edit.

| Field | Type | Default | Rules |
|---|---|---|---|
| `name` | string | required | `^[a-z][a-z0-9-]{0,30}$`, and the same as its folder. Its tools are `mcp__agtop-<name>__*`. |
| `description` | string | | Shown at approval and in `agtop plugin list`. |
| `version` | string | | Shown at approval; sent as the MCP server version. |
| `command` | string[] | required | The program and its arguments. The program is a path inside the plugin folder (no `..`), or absolute (an interpreter). It is run from the plugin folder. |
| `protocol` | `"agtop"` \| `"mcp"` | `"agtop"` | `mcp` means an MCP stdio server. It can't have `sessions`. |
| `env` | object | | Extra environment. `${DATA}` expands to the data folder and `${PLUGIN}` to the plugin folder, and a leading `~/` to the user's home (the plugin's own `HOME` is its data folder), so it can find the files it was given in `read` or `write`. It can't override `PATH`, `HOME`, `TMPDIR`, `AGTOP_*`, or (with `network`) the proxy variables. |
| `tools` | bool | false | Offer its tools to every agtop-mode session. Always on for `mcp`. |
| `sessions` | string[] | | Any of `list`, `start`, `read`, `send`, `control`, `queue`. See `protocol.md`. |
| `workspaces` | string[] | | Absolute paths or `~/…`, not `/`. Required with `start` and `queue`. |
| `network` | string[] | | `host:port` pairs: exact names, or public IPs, no wildcards. HTTPS through the proxy only. |
| `read` | string[] | | Extra absolute paths it may read, for an interpreter's libraries or another tool's files. |
| `write` | string[] | | Extra absolute paths it may write (and read), beyond its data folder: another tool's files. Not `/` or your home folder. |
| `exec` | object | | Programs agtop runs for it **outside the sandbox, as the user**, by name (`^[a-z][a-z0-9-]{0,40}$`): each a command line whose program is an absolute path or `~/…`. The plugin adds arguments with the `exec` call. Not for `mcp` plugins. |
| `memoryMB` | int | 256 | 1–8192. The broker kills it above this footprint. |
| `agents` | object | | Subagents for every agtop-mode session, keyed by name (`^[a-z][a-z0-9-]{0,40}$`), each as Claude Code's `--agents` takes them. `description` and `prompt` are required; optional `tools`, `model`. They appear as `<plugin>:<name>`. 64 KB in all. |
| `prompt` | string | | Added to every agtop-mode session's system prompt under a heading naming the plugin. 16 KB at most. |

Agents, prompt text and tools reach sessions **as approved**. Editing `plugin.json` changes nothing until the user approves again, and until then the plugin doesn't run.

## The environment the plugin gets

These values are fixed; the manifest can't change them:

| Variable | Value |
|---|---|
| `HOME`, `AGTOP_PLUGIN_DATA` | `<agtop>/plugin-data/<name>`, the only writable folder |
| `TMPDIR` | `<data>/tmp/` |
| `PATH` | `/usr/bin:/bin` (there's little point: it can't start programs) |
| `AGTOP_PLUGIN` | its name |
| `AGTOP_IPC_FD` | `3` (agtop plugins only) |
| `HTTPS_PROXY`, `HTTP_PROXY`, `ALL_PROXY` (and lowercase), `NODE_USE_ENV_PROXY=1` | its proxy, when it has `network` |

`LANG`, `LC_ALL` and `TZ` pass through from agtop. Nothing else does: no tokens, no `CLAUDE_*`, no user `PATH`.

## Examples

**An agtop plugin in Go** (a static binary; the most robust choice):

```json
{
  "name": "notes",
  "description": "Notes Claude keeps across sessions.",
  "version": "1",
  "command": ["notes"],
  "tools": true,
  "sessions": ["list"],
  "memoryMB": 32
}
```

**Python / Node** (Homebrew's interpreter; its libraries are readable automatically):

```json
{ "command": ["/opt/homebrew/bin/python3", "-I", "plugin.py"] }
{ "command": ["/opt/homebrew/bin/node", "plugin.mjs"] }
```

**An off-the-shelf MCP server** (`npm install @modelcontextprotocol/server-memory` inside the plugin folder):

```json
{
  "name": "memory",
  "description": "A knowledge graph Claude keeps across sessions.",
  "protocol": "mcp",
  "command": ["/opt/homebrew/bin/node", "node_modules/@modelcontextprotocol/server-memory/dist/index.js"],
  "env": { "MEMORY_FILE_PATH": "${DATA}/memory.jsonl" },
  "prompt": "You have a long-term memory in the mcp__agtop-memory__* tools. Search it at the start of a task; record durable facts."
}
```

**A plugin that runs agents of its own:**

```json
{
  "name": "delegate",
  "command": ["delegate"],
  "tools": true,
  "sessions": ["list", "start", "read", "send", "control"],
  "workspaces": ["~/Source"],
  "agents": {
    "reviewer": {
      "description": "Reviews a change for correctness bugs. Use after a non-trivial change.",
      "prompt": "You review code changes for correctness …",
      "tools": ["Read", "Grep", "Glob", "Bash"]
    }
  }
}
```

**One that works with another tool on your machine** (kanban-code, here): it reads the tool's files, drives its CLI, starts a session per card in its own worktree, and queues review feedback to the sessions working the cards:

```json
{
  "name": "kanban",
  "command": ["kanban"],
  "tools": true,
  "sessions": ["list", "start", "read", "queue"],
  "workspaces": ["~/Source"],
  "read": ["~/.kanban-code"],
  "exec": { "kanban": ["~/.local/bin/kanban", "--json"] },
  "prompt": "Your task may be a kanban card: call mcp__agtop-kanban__my_card to read it."
}
```

`exec` and `write` reach outside the sandbox, and approval says so. Name the narrowest program, and prefer `read` to `write`.

**One that calls an API:**

```json
{
  "name": "linear",
  "command": ["linear"],
  "tools": true,
  "network": ["api.linear.app:443"],
  "env": { "LINEAR_KEY_FILE": "${DATA}/key" }
}
```

Secrets: the plugin can't read the user's files or environment, so a key must live in its data folder. Ask the user to put it there (e.g. `~/.config/agtop/plugin-data/linear/key`) rather than putting it in `plugin.json`: the manifest is shown at approval and is part of the plugin's folder.
