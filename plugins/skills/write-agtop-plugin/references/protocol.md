# The agtop plugin protocol (version 1)

## Transport

**agtop plugins** (`"protocol": "agtop"`, the default): fd 3 is one end of a unix socket pair agtop created for the plugin. `AGTOP_IPC_FD=3` says so. Nothing else can connect to it. Every message, both ways, is:

```
[4 bytes: length N, big-endian uint32][N bytes: one JSON-RPC 2.0 message, UTF-8]
```

N is at most 16 MB; a bigger frame closes the connection. There is no newline, and nothing to escape.

**MCP plugins** (`"protocol": "mcp"`): standard MCP stdio, one JSON message per line on stdin and stdout. agtop initializes the server once, with protocol version `2025-06-18` and no client capabilities, and forwards `tools/list` and `tools/call` from every session to that one process. It offers no sampling, roots or elicitation. The server's `instructions`, if any, go to each session.

JSON-RPC 2.0 throughout: a request has `id`, `method`, `params`; a reply has the same `id` and either `result` or `error: {code, message}`; a notification has no `id` and gets no reply. **Both sides send requests**, so an agtop plugin must tell a reply (no `method`) from a request (`method` and `id`) from a notification (`method`, no `id`). Handle requests concurrently. Notifications arrive in order.

## Lifecycle

1. The broker checks the plugin's files against the approval, starts it (sandboxed, at utility QoS), and sends `initialize`.
2. The plugin answers within 15 seconds. It is then "running", and sessions' tool calls reach it.
3. When fd 3 closes, the plugin exits. If the plugin exits, or its fd 3 closes, or it goes over its memory limit, the broker starts it again after 1s, 2s, 4s … up to 60s, resetting once a run lasts a minute.
4. If its files change, the broker stops it and doesn't start it again until the user re-approves.

## agtop → plugin

### `initialize` (request)

```json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{
  "protocol": 1,
  "name": "notes",
  "dataDir": "/Users/you/.config/agtop/plugin-data/notes",
  "sessions": ["list"],
  "workspaces": ["/Users/you/Source"],
  "network": [],
  "exec": []
}}
```

Answer `{}` (anything but an error). `sessions`, `workspaces`, `network` and `exec` (the names of the programs it may run) are what the plugin was approved for; `workspaces` are resolved to real paths.

### `tools.list` (request)

Answer `{"tools": [ ... ]}`, MCP tool definitions:

```json
{"name": "note", "description": "Keep a note for later sessions.",
 "inputSchema": {"type": "object", "properties": {"text": {"type": "string"}}, "required": ["text"], "additionalProperties": false}}
```

Tool names: letters, digits, `_` and `-`. Claude sees them as `mcp__agtop-<plugin>__<tool>`, 64 characters at most in all.

### `tools.call` (request)

```json
{"jsonrpc":"2.0","id":7,"method":"tools.call","params":{
  "session": "a1b2c3d4",
  "name": "note",
  "arguments": {"text": "the build needs Go 1.27"}
}}
```

`session` is the id of the agtop session whose Claude called the tool. When agtop knows it, the call also carries `sessionId` (Claude Code's id for the conversation, the one its transcript and hooks use), `cwd`, and `meta` (what the session was started with, see `sessions.start`). They tell the plugin which of its things the caller is: a `my_task` tool, say, looks up the task by `meta` or `sessionId` rather than asking Claude. Answer an MCP `CallToolResult`:

```json
{"content": [{"type": "text", "text": "Noted."}], "isError": false}
```

Tool failures are `isError: true` results with a message Claude can act on. Reserve JSON-RPC errors for broken requests. A call may take up to 10 minutes. The user is asked to allow each call before it reaches the plugin.

### `session.event` (notification)

This is sent for each session the plugin follows with `sessions.subscribe`. It covers only what happens after the subscription, not history. `params.session` is the id, and `params.type` is one of:

| `type` | Other fields | Meaning |
|---|---|---|
| `info` | `state`, `detail`, `needs`, `costUsd` | Its state changed. `state` is `starting`, `working`, `blocked` (waiting for the user), `idle` or `stopped`. `detail` is what it's doing in words; `needs` is what it's blocked on. |
| `sent` | `text` | A message was sent to it (by anyone). |
| `text` | `text` | Claude said this (main thread, not subagents). |
| `tool` | `name`, `doing` | Claude used a tool; `doing` says what, in words. The tool's output is never sent. |
| `result` | `text`, `isError`, `costUsd`, `turns` | A turn ended; `text` is Claude's final answer. |
| `closed` | | The session's host went away; subscribe again once it's back if needed. |

## plugin → agtop

Each call is checked against the approved manifest. A refused call gets error **`-32001`** with a message saying why. Session ids are short strings like `a1b2c3d4`.

### `sessions.list` — needs `list`

No params. Returns every agtop-mode session:

```json
[{"id": "a1b2c3d4", "sessionId": "5f0c…-…", "name": "fix the flaky test",
  "cwd": "/Users/you/Source/app/.claude/worktrees/flaky", "repo": "/Users/you/Source/app/.claude/worktrees/flaky",
  "branch": "fix-flaky", "worktree": true,
  "state": "working", "detail": "running go test ./...", "needs": "", "model": "claude-opus-5-5",
  "permissionMode": "acceptEdits", "costUsd": 0.42, "contextTokens": 81234, "queued": 1,
  "startedBy": "kanban", "meta": {"card": "card_2x…"}, "startedAt": "…", "updatedAt": "…"}]
```

- `sessionId` is Claude Code's id for the conversation, empty until it has started. Other tools (Claude Code's hooks, transcripts in `~/.claude/projects`, anything that reads them) know the session by it.
- `repo` is the top of the git checkout `cwd` is in, `branch` what it has checked out (the first 8 characters of a commit when detached), and `worktree` is true for a linked worktree.
- `contextTokens` is how much context the last request sent, the conversation's size as the model sees it. `queued` is how many messages wait for its turn to end.
- `startedBy` is the plugin that started it, or empty; `meta` is what that plugin tagged it with.

What was said is never included.

### `sessions.watch` / `sessions.unwatch` — needs `list`

`sessions.watch` takes no params and returns the list, as `sessions.list` does. From then on, agtop checks every 2 seconds and sends:

| Notification | `params` | When |
|---|---|---|
| `session.changed` | a session, as in the list | one appeared, or anything about it changed |
| `session.gone` | `{"id": "…"}` | its host's files were removed (a stopped session stays, as `"state": "stopped"`) |

Call it once `initialize` has been answered (before then it fails; ask again). Watching again replaces the old watch. `sessions.unwatch` ends it. It also ends when the plugin restarts.

### `sessions.start` — needs `start`

```json
{"cwd": "/Users/you/Source/app", "prompt": "Update the changelog for 2.3",
 "name": "changelog", "model": "sonnet", "effort": "medium", "permissionMode": "acceptEdits"}
```

Two more fields, both optional:

- `worktree: {"name": "…", "branch": "…", "base": "…"}` runs the session in a new git worktree of the checkout `cwd` is in, at `<checkout>/.claude/worktrees/<name>`, where Claude Code puts its own. `branch` is the new branch (`worktree-<name>` if left out) and `base` what it starts from (`HEAD` if left out). `name` defaults to one made from the branch or the session's name. The checkout must be inside the plugin's workspaces too.
- `meta: {"card": "card_2x…"}` tags the session, and comes back in `sessions.list`, `session.changed` and every `tools.call` from it. At most 16 keys, of letters, digits and `_ . -`, with values of at most 1 KB.

Only `cwd` and `prompt` are required. It returns `{"id": "…", "cwd": "…"}`, `cwd` being where the session runs (the worktree's folder, if it made one). What agtop enforces:

- `cwd` must be absolute, exist, and be inside one of the plugin's `workspaces`.
- `permissionMode` must be `default` (the default), `acceptEdits` or `plan`.
- `effort` must be `low`, `medium`, `high`, `xhigh` or `max`.
- The prompt is at most 100 KB.
- The session is named `<plugin>: <name>`, runs on the user's account and defaults, and is marked as the plugin's.
- A plugin can have at most 4 sessions running at once, and start at most 30 an hour.

### `sessions.send` — needs `send`, own sessions only

`{"id": "…", "text": "…", "now": false}`. When the agent is busy, the message queues and goes when its turn ends; `now: true` delivers it mid-turn instead. Text is at most 100 KB. Returns `{}`.

### `sessions.queue` — needs `queue`

`{"id": "…", "text": "…"}`. Queues a message to **any** session in the plugin's workspaces, not only its own, as a queued message of yours would be: it goes when the turn ends, or now if the session is idle. Returns `{}`. What agtop enforces:

- The session runs in one of the plugin's workspaces (a session it started always may).
- Its permission mode asks the user first: `default`, `acceptEdits` or `plan`. A session in `bypassPermissions` or `auto` is refused.
- The text is at most 100 KB, and goes with a first line saying who it's from, `[from the agtop plugin <name>]`, so neither Claude nor the user takes it for the user's.

### `sessions.subscribe` / `sessions.unsubscribe` — needs `read`, own sessions only

`{"id": "…"}`. Returns `{}`; events follow as `session.event`. Subscribing again replaces the old subscription. Subscriptions end when the plugin restarts.

### `sessions.interrupt` / `sessions.stop` — needs `control`, own sessions only

`{"id": "…"}`. Interrupt stops the current turn; stop ends the session (its conversation is kept, and the user can resume it). Returns `{}`.

### `exec` — needs the program in `exec`

```json
{"name": "kanban", "args": ["show", "card_2x…"], "stdin": "", "cwd": "/Users/you/Source/app"}
```

Runs a program the manifest names in `exec`, **outside the sandbox, as the user**: its fixed command line, then `args`. `cwd` must be inside the plugin's workspaces; without it, the program runs in the plugin's data folder. It gets the user's environment. It returns when the program exits:

```json
{"code": 0, "stdout": "…", "stderr": "", "truncated": false}
```

A non-zero exit is a result, not an error. Limits: 64 arguments of at most 4 KB, 1 MB of stdin, the first 1 MB of each of stdout and stderr (`truncated` says if more was cut), a minute to run, and 4 programs at once.

### `log`

`{"message": "…"}`. This writes a line (at most 1 KB) to the broker's log. It works as a request or a notification. Plain stderr works too, and goes to the plugin's own log.

## Error codes

| Code | Meaning |
|---|---|
| -32700 | parse error |
| -32601 | method not found |
| -32602 | bad params (bad id, cwd doesn't exist, text too long …) |
| -32000 | something failed (the session isn't running …) |
| -32001 | not permitted: the capability wasn't approved, the session isn't the plugin's, the cwd is outside its workspaces, the program isn't in `exec`, a limit was hit |
