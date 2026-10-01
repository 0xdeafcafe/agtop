# Rooms

A room is a group chat on one topic: a fresh panel of agents (mixed models
argue best) takes turns, sees everything said, runs tools to check facts,
argues until it agrees, and posts one verdict back to you. You're in the room
the whole time and can speak at any moment.

```
 you ──#room / rush room new──▶ rooms/<id>/room.json   (topic, folder, panel, max rounds)
                                       │
                         rush room run <id>  (the driver: its own process)
                                       │ spawns one ordinary rush session per member
                     ┌─────────────────┼─────────────────┐
                  Haiku              Luna              Kimi       (host.Spawn, Owner = driver)
                     └──── turn by turn, round-robin ────┘
                                       │ posts every message, tool call, verdict
                                       ▼
                             rooms/<id>/log.jsonl  ◀── you post here too (view, rush room say)
                                       │
                   the fleet pane and rush room show tail it
```

## Turn-taking

Round 1 is blind openings: every member writes its opening at once, none
seeing the others', so nobody anchors on the first speaker. From round 2 it's
round-robin, one speaker at a time in panel order: every turn answers the
turns before it, by name.

```
round 1   Haiku ┐
          Luna  ├ at once, blind
          Kimi  ┘
round 2+  Haiku → Luna → Kimi → …        (you can cut in at any point)
end       Haiku ┐
          Luna  ├ at once: each one line, its own final position → verdict card
          Kimi  ┘
```

- **What a turn sees.** Each member is a persistent session, so it keeps its
  own history. Its turn's prompt is everything said since its last turn: the
  others' messages, the tools they ran (one line each), and anything you said.
  The first turn also carries the topic, the panel and the rules.
- **Opinions.** The rules tell each agent to check claims (read code, run
  read-only commands), challenge the others by name, and change its mind only
  when persuaded.
- **Tools.** The driver answers approvals: reading and running are allowed,
  changing files is refused ("this is a discussion"), questions go back
  unanswered. Tool calls are posted as they start and marked when they finish.
- **Convergence.** Every turn ends with `READY: yes` or `READY: no`. From
  round 2, when every member's latest turn said yes, and said it after you last
  spoke, the room has converged. At the round cap (4 by default) it ends
  anyway.
- **The verdict.** No one speaks for the room: when it ends (converged, cap,
  or your `/verdict`) every member states its own final position in one line,
  at once, and the verdict card lists them, a line each. Disagreement shows as
  differing lines.
- **Bounds.** A turn longer than 10 minutes is interrupted. Prompts carry at
  most 60 KB of the room, less when a member's model has a small context
  window (about 1.5 bytes a token of it); the oldest turns go first.

## You in the room

Nothing is private and the box is always live, mid-turn and paused included.

- A message goes straight into the log. If the speaker's agent takes a
  message mid-turn (`FeatureGuide`: Claude Code does), it's handed into the
  turn under way; otherwise the very next turn answers you first. You're never
  queued behind the rest of the round.
- `@name ...` goes to one member: it speaks straight after the current turn,
  in place of its turn this round.
- `/pause` (ctrl+p) interrupts whoever is speaking at once. What they'd said
  and the tools they'd run stay in the log, marked paused. No turn starts
  while paused, and the view says PAUSED; what you type meanwhile is what the
  agents read first. `/resume` (ctrl+p again) carries on: the interrupted
  agents pick their turns up again, in the same round. A pause never counts
  against the round cap.
- `/verdict` ends the discussion after this turn (a guided agent is asked to
  wrap up at once; it also lifts a pause) and every member gives its final
  position. `/stop` (ctrl+c) interrupts and ends the room without them.
- Speaking resets convergence: everyone has to say yes again after you.

## The view

A room is an item in the fleet list like any agent: one row, `◈ topic`, with
how it stands (who is speaking, openings, paused, verdict in, over) and its
member count. Its members' sessions sit under it as `└ Name` rows, never as
rows of their own. Enter or a click on the room, or on any of its members,
shows the room in the pane where an agent's conversation shows, with the list
beside it; `esc` goes back to the list with the room's row selected.

The pane draws the room with the conversation renderer every agent uses
(`roomFold` in `internal/ui/roomfeed.go` turns the log into a session): your
messages as yours, each member's words and tool calls as a turn headed by its
name, agent and round, the verdict as the last turn with one line per member.
The header says how the room stands, `PAUSED` included. The box is an agent's
box, always live: a message, `@name`, `/pause`, `/resume`, `/verdict`,
`/stop`; `ctrl+x` pauses, as it stops an agent's turn. `alt+1…9` opens a
member's own session in the same pane.

`#room` selects the newest room. `#room new` or `#room <topic>` sets one up
(topic, panel: Tab to the list, Space to choose, ←→ for the round cap), and
Enter opens it in the pane. `#discuss` opens a room too. `ctrl+x` → `y` on the
room's row hides it and its members for good (stopping it if it still runs);
its log stays on disk.

The pane only tails the log and posts to it, through tea.Cmds; nothing in
Update or View touches the disk.

## CLI

```sh
rush room new --cwd . --agent claude:haiku --agent codex:gpt-6-luna:low --agent kimi "topic" [--rounds N] [--watch]
rush room show <id> [--follow]
rush room say <id> "@kimi what about X?"     # or /verdict, /stop, /pause, /resume
rush room pause <id>                          # and resume, verdict, stop
rush room list
```

## Limits

- The driver can't resume: if it dies, the room's sessions end with it
  (`Owner`) and the room shows as over.
- Shell commands aren't vetted beyond the agent's own permission mode; only
  file-changing tools are refused.
- Pausing interrupts a turn; an agent's partial work is kept in its own
  session, but the resumed turn is a new one, told to pick up where it was.
