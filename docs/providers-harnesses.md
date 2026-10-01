# Providers, harnesses, models: one rush

Status: agreed 2026-09-30; Settings and Starting redesigned 2026-10-01 (see below).

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

Compatibility is the provider's: Anthropic's subscription runs in Claude
Code, OpenAI's in Codex, and either in Pi on Pi's own sign-in (with a
warning); API-key and local providers run in any harness that speaks their
API. `agent.Compat` says which, and why not.

## Settings: Providers, Harnesses, Profiles

The detail, mockups and matrix are in
[providers-redesign.md](providers-redesign.md).

- **Providers**: providers only (Anthropic's and OpenAI's subscriptions apart
  from their API keys), each with its accounts or key and limits. Beside the
  one picked: its harnesses (★ where new sessions start; any other that can
  run it, a plan through another's sign-in with a warning), what new
  sessions start with, what it does at a limit, and at the bottom what rush
  can do with it and its models, as before.
- **Harnesses**: each program; beside it the provider it previews, every
  provider it runs on (★, ⚠ or why not), its sign-in and its own settings,
  then the pairing's features.
- **Profiles**: your profiles, then the folders that pick one.

## Starting and switching

- `#new [harness][@provider[:account]] [model] [effort] [task]` starts one
  session on any route, once: `#new codex@openai-sub gpt-6-astra high fix the
  test`. Words complete as you type and the border says what it starts, or
  why it can't. Without a task it's the next session's; `#with` is that.
- The start sheet (shift+tab, alt+m) is three linked columns, Provider,
  Harness and Model, each greying what the others can't run, with why.
- Neither changes a default: Settings does.

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
