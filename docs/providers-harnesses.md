# Providers, harnesses, models: one rush

Status: proposed, 2026-09-30. Replaces Settings' Providers and Capabilities pages.

## Principle

You use rush, not Claude Code or Codex. A session is a model from a provider,
run by a harness. rush names things by provider, account and model; the
harness is a detail you can see, never the headline.

## The three things

| Thing    | What it is                                  | Examples |
|----------|---------------------------------------------|----------|
| Provider | who serves the model, and how you pay       | Anthropic subscription, Anthropic API, OpenAI subscription, OpenAI API, Ollama, GLM, DeepSeek, Kimi, Mistral, Gemini |
| Harness  | the program that runs the session           | Claude Code, Codex, Pi, OpenCode, Vibe, Gemini CLI, Kimi CLI |
| Model    | what the provider serves, from its own list | opus[1m], gpt-6-astra, qwen3-vl:30b |

Accounts belong to a provider and are named by you (alex, work).

Compatibility is the provider's: Anthropic subscription runs only in Claude
Code; OpenAI subscription only in Codex; API-key and local providers run in
any harness that supports them. rush only ever offers valid combinations.

## Settings > Providers (one page)

Keep today's layout and look; sharpen it. Remove what's said twice (the
summary line repeated as "spend", "its own sign-in" under every row, "can
do" as a truncated row pointing at another page) and anything that doesn't
help a decision.

- Left: providers only, each with its accounts and status.
- Right, for the selected provider:
  - Accounts (named; add, sign in, limits, spend).
  - Harnesses: every harness this provider can run in, which ones you use,
    and one marked default.
  - Defaults per provider x harness: model, effort, permissions.
  - What it can do: a plain list for the selected provider and its default
    harness (the old Capabilities matrix is removed).
- Profiles: named setups of provider (+ account), harness, model and effort,
  shown as `<name>` or `<harness>:<account>` (codex:alex, claudecode:alex).
- Folder rules stay: a folder picks a profile.

## Starting and switching

- The input shows what the next session starts as, as a chip.
- The start sheet (alt+m): Profile, or Provider/Account > Harness > Model >
  Effort. Only valid combinations. For that session only; defaults untouched.
- Commands, with autocomplete everywhere:
  - `/profile <name>`
  - `/agent <harness-or-provider>:<account>[:<effort>]`
  - typed in the input, they set the next session; in a session, they switch it.
- In a session: the header's chip opens the same sheet. Same harness: model,
  account and effort switch in place. Another harness: the conversation is
  handed over (as handoff does today).

## Usage everywhere

- The header's usage bar shows every provider with limits, compressed: one
  mini meter each (Claude, Codex, Copilot, Gemini, Vibe…), the lowest first
  when there isn't room.
- A session's header names its agent (profile or harness:account and
  model); when its usage runs low it says where you could switch to, the
  provider or account with the most room.
- Vibe's and Gemini's limits are read like Claude's and Codex's, from what
  their own programs report; Gemini's free allowance shows as such.

## Stalls

- Claude Code sessions rush hosts run with Claude Code's stream watchdog on
  (`CLAUDE_ENABLE_STREAM_WATCHDOG=1`), so a silent stream is cut and retried.
- rush marks a turn with no stream activity for 2 minutes as stalled, on its
  working line, with a key to interrupt and send again.
- Settings > General says what's on.

## Order of work (run in parallel where files don't overlap)

1. Stalls: watchdog on, stalled marker, settings line.
2. Model: provider identities split by billing; provider > harness use and
   default; provider x harness defaults; profiles carry provider, account,
   harness, model, effort. Older configs still read.
3. Settings page as above; Capabilities page removed.
4. Start sheet, `/profile` and `/agent` with autocomplete, in-session switch,
   `<harness>:<account>` names.
