# Providers, harnesses and models: the redesign

Status: approved and built, 2026-10-01, with the rulings in §7. The detail
behind `providers-harnesses.md`, whose Settings and Starting sections link
here.

## What Alex asked for

- Launch any valid combination from the input, one-off: Codex on the OpenAI
  subscription, Claude Code on the Anthropic subscription, Pi on Ollama.
  It never changes a default.
- Three separate things: **providers** (where access and billing come from),
  **harnesses** (the agent program) and **models** (what a provider serves).
- Two clean Settings tabs: Providers (each provider, its harnesses, ★ the
  default) and Harnesses (each harness, what it can run on).

## 1. How today's code mixes the three

The root: an `agent.Kind` is one adapter, and one adapter is a provider *and* a
harness at once. Everything else follows from that.

| # | Where | What is mixed |
|---|-------|---------------|
| 1 | `internal/agent/harness.go:8-13`, `internal/adapters/cross/cross.go:44-52`, `internal/adapters/ollama/{pi,codex,vibe}.go` | A provider in a harness is a kind of its own (`ollama-pi`, `anthropic-pi`, `deepseek-claude`). There are 6 cross kinds and 4 Ollama kinds, and every new pair adds a kind. |
| 2 | `internal/adapters/ollama/ollama.go:21-29` | Kind `ollama`, named "Ollama", is really *Claude Code on Ollama*. The provider id and one of its pairs share a name. |
| 3 | `internal/agent/harness.go:76-93` | A provider's default harness comes from naming: the kind spelt like the provider sorts first. Ollama defaults to Claude Code only because kind `ollama` rides Claude Code. |
| 4 | `internal/agent/harness.go:38-46` | `ProviderOf` returns the kind itself when an adapter isn't a rider. So **Pi and OpenCode are providers** ("Pi", "OpenCode"), and ZCode is provider "GLM", dsh is "DeepSeek", Kimi CLI is "Moonshot". A harness that signs in by itself turns up as a provider. |
| 5 | `internal/agent/harness.go:179-214` | Billing is spelt into the provider id with a suffix (`claude-key`, `codex-key`). The subscription's id is the harness kind (`claude`, `codex`). |
| 6 | `internal/state/state.go:384-420` | Start defaults (`Dispatch.Starts`) are keyed by **kind**. Anthropic subscription and Anthropic API key both run kind `claude`, so they share one default model, effort and mode. Claude's live in the legacy `Dispatch.Model/Effort/Permission` fields. |
| 7 | `internal/state/profiles.go:26-58` | `Profile` holds providers, per-provider harness (`RunsIn`) and a policy, and then `Billing`, `Account`, `Model` and `Effort`, which apply to the **first** provider only. |
| 8 | `internal/state/profiles.go:156-182` | `ProfileNamed` treats any provider id *or* agent kind as a profile. `#profile ollama-pi` picks a harness through the profile mechanism. |
| 9 | `internal/state/profiles.go:541-575`, `internal/ui/settings_providers.go:627-680` | Each harness is "used / not used" per provider (`Config.Harnesses`) on top of the default (`Config.RunsIn`). That's a third state Alex doesn't want: any harness that supports a provider should be launchable. |
| 10 | `internal/ui/settings.go:38,50-58` | Pages: Models, Harnesses and Accounts. `pageProviders` is labelled "Models". `accountsPage()` is `providersPage()` renamed (`settings_providers.go:55-59`). |
| 11 | `internal/ui/settings_providers.go:70-95` | The Accounts list mixes providers, **profiles** and **folder rules** in one column. |
| 12 | `internal/ui/settings_models.go:122-191` | Models is grouped by provider, and each group ends in "Accounts and provider settings" and "Routing", which open the whole Accounts page *inside* Models (`d.modelAccounts`, lines 13-24, 165-189). That's a page within a page, with its own esc stack. |
| 13 | `internal/ui/settings_models.go:205-227` | "Use for new sessions" on a model writes that kind's default model **and** changes the provider's default harness (`SetRunsIn`, line 220). You pick a model and a provider default changes as a side effect. |
| 14 | `internal/ui/settings_harnesses.go:81-109` | A harness's "Provider" control is a picker of *kinds*. It then shows accounts and `agentSections`, the same start defaults the Accounts and Models pages show. |
| 15 | `internal/ui/settings_agent.go:121`, used from `settings_providers.go:611`, `settings_harnesses.go:107`, `settings_models.go:205` | One default (`Dispatch.Starts[kind]`) is edited from three pages, each with different words. |
| 16 | `internal/ui/settings_providers.go:498-517` | "What it can do" (the feature grid) sits under a *provider*. Features are mostly the harness's (`cross.go:69-90` derives a pair's features from its harness). |
| 17 | `internal/ui/startsheet.go:36-67` | The start sheet drops invalid combinations instead of greying them, so you can't see why Pi isn't offered for the Anthropic subscription. |
| 18 | `internal/ui/startsheet.go:147,447-487` | The rows are Profile > Provider > Harness > Account > Model > Effort > Permissions, one value at a time with ‹ ›. Changing the provider or harness resets the model and effort, and you can't see the matrix. |
| 19 | `internal/ui/setup.go:20-57` | The `/agent` grammar is `<provider>-<harness>:<account>[:<effort>]`. The provider word comes from the company and the harness word from the kind, so GLM's harness is "glm" (it's ZCode), DeepSeek's is "deepseek" (it's dsh), and there's no **model** slot. |
| 20 | `internal/ui/with.go:11-37` | `#with <kind>` **changes the default** (`SetDefaultProvider`). Meanwhile `#profile` (`profiles.go:82`) and `/agent` (`setup.go:499`) only change the next session. That's three commands with two meanings and two prefixes. |
| 21 | `internal/ui/setup.go:286-304` | A one-off account (`/agent claudecode:work`) swaps the harness's signed-in account for **every** session of it. A one-off quietly changes global state. |
| 22 | `internal/ui/providers.go:29-42,50-63` | Glyphs are keyed by kind. The table mixes provider marks (OpenAI, DeepSeek) with harness marks (π Pi, ▣ OpenCode). Anthropic in Pi gets the generic ◇ because `looks["claude"]` doesn't exist. |
| 23 | `docs/multi-agent.md:24` vs `internal/state/profiles.go:25` | "Profile" means two things: `agent.Profile` (a config home) and `state.Profile` (a routing policy). |

## 2. The model

```
Provider  ── where access and billing come from (subscription, API key, local)
  └ Account / Key ── who pays: named sign-ins, or one kept key
Harness   ── the program that runs the session (Claude Code, Codex, Pi …)
Model     ── from the provider's catalogue, as the harness can reach it

A session  =  Provider(+account)  ×  Harness  ×  Model  (+ effort, permissions)
```

- **A route** is `provider/harness`. It's the unit of compatibility and of
  defaults. Internally it still resolves to today's adapter kind through one
  function (`agent.KindFor` already does most of it), so **no adapter is
  rewritten**. Kinds become an implementation detail that nothing user-facing
  shows or stores.
- **Compatibility** is one seam, `agent.Compat(provider, harness) (kind,
  reason)`. It returns the adapter or a reason in words. The start sheet,
  Settings and `#new` all read it. Today's `routes()` drops invalid
  combinations; `Compat` keeps them with their reason.
- **Defaults** (written only in Settings):
  - one default provider (today's default profile, unchanged)
  - one default **harness per provider** (today's `Config.RunsIn`, unchanged)
  - one default **model, effort and permission mode per route**
    (`Dispatch.Starts`, re-keyed from kind to route)
- **Overrides** (`#new`, `/agent`, the start sheet) apply to one session and
  never write any of the above.

### Providers (user-facing words; stored ids unchanged, see question 5)

| Provider | Typed as | Paid by | Accounts | Stored id today |
|---|---|---|---|---|
| Anthropic · subscription | `anthropic-sub` | Claude plan | named sign-ins | `claude` |
| Anthropic · API key | `anthropic-api` | per token | one key | `claude-key` |
| OpenAI · subscription | `openai-sub` | ChatGPT plan | named sign-ins | `codex` |
| OpenAI · API key | `openai-api` | per token | one key | `codex-key` |
| GitHub Copilot | `copilot` | seat | gh accounts | `copilot` |
| Google | `google` | Google sign-in | its own sign-in | `antigravity` |
| Mistral | `mistral` | Vibe plan | its own sign-in | `vibe` |
| Moonshot (Kimi) | `moonshot` | Kimi plan | its own sign-in | `kimi` |
| DeepSeek | `deepseek` | per token | one key | `deepseek` |
| GLM (Z.ai) | `glm` | Coding Plan key | one key | `glm` |
| Ollama | `ollama` | free, local | none | `ollama` |

Pi and OpenCode stop being providers. When either is signed in through its own
`/login`, that shows as an account under the harness (question 1).

### Compatibility matrix (what the code supports today; per-adapter detail in `agent-compatibility-matrix.md`)

`★` default harness for the provider · `✓` launchable · `⚠` launchable, with a warning on the border · `–` not possible (reason under the table) · `◌` possible later

```
                     Claude Code  Codex   Pi    OpenCode  dsh   ZCode  Copilot  Kimi  Vibe  Antigravity
anthropic-sub            ★          –      ⚠ a     ◌ a      –     –      –       –     –       –
anthropic-api            ★          – b    ✓       ◌        –     –      –       –     –       –
openai-sub               – a        ★      ⚠ a     ◌ a      –     –      –       –     –       –
openai-api               – c        ★      ✓       ◌        –     –      –       –     –       –
copilot                  –          –      –       –        –     –      ★       –     –       –
google                   –          –      ◌       –        –     –      –       –     –       ★
mistral                  –          –      –       –        –     –      –       –     ★       –
moonshot                 ◌ d        –      ◌       –        –     –      –       ★     –       –
deepseek                 ✓          – b    ✓       ◌        ★     –      –       –     –       –
glm                      ✓          – b    ✓       ◌        –     ★      –       –     –       –
ollama                   ★          ✓      ✓       ◌        –     –      –       –     ✓       –
```

- a: a plan runs in its own program, or in Pi on Pi's own `/login` (ruling Q1:
  allowed, with "runs your Anthropic plan through Pi" on the border). OpenCode
  can sign in to a plan too; rush doesn't run that yet.
- b: Codex speaks only OpenAI's Responses API, which these providers don't serve.
- c: Claude Code speaks only Anthropic's API, which OpenAI doesn't serve.
- d: Moonshot serves an Anthropic-compatible API; adding it is one more `cross` pair.

## 3. Launching from the input

### The syntax: `#new`

```
#new [harness][@provider[:account]] [model] [effort] [task…]
```

- Every part is optional. Whatever is left out comes from the defaults, in
  this order: the provider's default harness, then the route's default model,
  effort and mode.
- A part is taken only when it **exactly** matches a known word. The first word
  that doesn't match starts the task. Quote the task if it begins with a word
  like `high`.
- `harness` alone (`#new pi …`) runs on the next session's provider when that
  harness can run it. Otherwise it uses the first configured provider whose
  default harness this is, then any compatible provider. The preview always
  shows which one it picked.
- `@provider` alone (`#new @ollama …`) uses that provider's ★ harness.
- `model` alone (`#new gpt-6-astra …`) takes the provider from the catalogue
  that has it, preferring a subscription with room, then the ★ harness.
- Shorthands: `cc` = Claude Code; `@sub` and `@api` = the harness's own
  provider by subscription or by key (`codex@sub`, `cc@api`).
- With a task, it **starts now**. Without one, it sets the next session (the
  chip), as `/agent` does today.
- In a Session, `#new` always starts a **new** session. `/agent` stays the
  in-session switch and accepts the same target grammar
  (`/agent codex@openai-sub:alex:high`). The old `openai-codex:alex` and
  `codex:alex` names still parse.

Examples:

```
#new codex@openai-sub gpt-6-astra high fix the flaky retry test     Codex · OpenAI subscription, one-off
#new cc@anthropic-sub:work opus[1m] max "high-risk migration plan"   Claude Code on the work account
#new pi@ollama qwen3-coder tidy imports                              Pi on local Ollama
#new @deepseek explain this stack trace                              DeepSeek in its ★ harness (dsh)
#new gpt-6-sol                                                       next session only: Codex, OpenAI sub
```

### Mockup: typing, with completion and a live preview

```
┌ Agents ──────────────────────────────────────────────────────────────────────────────────────────────────────────┐
│  …                                                                                                               │
│ ┌ harness@provider ────────────────────────────────────────────────────────────────────────────────────────────┐ │
│ │ ▸ codex@openai-sub    ◎ Codex · OpenAI subscription · alex    gpt-6-astra · high    ★ OpenAI sub's           │ │
│ │   codex@openai-api    ◎ Codex · OpenAI API key                ✗ no key yet: $ in Settings › Providers        │ │
│ │   codex@ollama        ◎ Codex · Ollama                        qwen3-coder:30b                                │ │
│ │   copilot@copilot     ◈ Copilot CLI · GitHub Copilot · gh     gpt-6-sol             ★ Copilot's              │ │
│ │   cc@anthropic-sub    ✻ Claude Code · Anthropic subscription  opus[1m] · high       ★ fuzzy match            │ │
│ └─────────────────────────────────────────────────── ↑↓ choose · tab completes · alt+m opens the start sheet ──┘ │
╭──────────────────────────────────────────────────────────────────────────────────────────────────────────────────╮
│ #new co▏                                                                                                         │
╰─ ✻ Claude Code · Anthropic subscription · alex · opus[1m] · high ──────────────────────────────── the default ───╯
```

After the target and a space, the list switches to that route's models, then
its efforts:

```
│ ┌ model · codex@openai-sub ────────────────────────────────────────────────────────────────────────────────────┐ │
│ │ ▸ gpt-6-astra    400k   the most capable, for hard, long-running work               ★ default here           │ │
│ │   gpt-6-sol      400k   the workhorse                                                                        │ │
│ │   gpt-6-luna     200k   fast and cheap                                                                       │ │
│ └────────────────────────────────────────────────────────── tab completes · a word not listed starts the task ─┘ │
╭──────────────────────────────────────────────────────────────────────────────────────────────────────────────────╮
│ #new codex@openai-sub gpt-6-astra high fix the flaky retry test in internal/host▏                                │
╰─ ◎ Codex · OpenAI subscription · alex · gpt-6-astra · high · one-off, defaults untouched ──── enter starts it ───╯
```

An invalid combination is caught before enter, on the border, with the way out:

```
╭──────────────────────────────────────────────────────────────────────────────────────────────────────────────────╮
│ #new pi@anthropic-sub tidy imports▏                                                                              │
╰─ ✗ Anthropic's subscription runs only in Claude Code · try cc@anthropic-sub or pi@anthropic-api ─────────────────╯
```

### Mockup: the start sheet (shift+tab or alt+m, also from `#new`)

Three linked columns. Pick in any order: choosing a harness first greys out
the providers it can't run, and the other way round. Invalid cells stay
visible, greyed, with the reason. The last line is the `#new` command that
does the same thing, so the sheet teaches the syntax.

```
╭─ Start as ──────────────────────────────────────────────── the next session only; Settings stay as they are ─╮
│ Profile   ‹ none: as below ›                                                                                 │
│                                                                                                              │
│ PROVIDER                            HARNESS                               MODEL                              │
│   ✻ Anthropic · subscription  ★cc   ▸ ◎ Codex          ★  0.130.0         ▸ gpt-6-astra   400k  ★            │
│   ✻ Anthropic · API key       ✓       ✻ Claude Code    –  sub: Codex only   gpt-6-sol     400k               │
│ ▸ ◎ OpenAI · subscription   ★codex    π Pi             –  sub: Codex only   gpt-6-luna    200k               │
│   ◎ OpenAI · API key        no key    ▣ OpenCode       –  sub: Codex only                                    │
│   ◈ GitHub Copilot          ★         ◆ dsh            –  DeepSeek only                                      │
│   ◆ DeepSeek                ✓         … 5 more         –                                                     │
│   ▲ GLM (Z.ai)              ✓                                                                                │
│   ◉ Ollama                  ★cc                                                                              │
│                                                                                                              │
│ Account  ‹ alex, in use ›      Effort  ‹ high ›      Permissions  ‹ auto ›                                   │
│                                                                                                              │
│ ◎ Codex · OpenAI subscription · alex · gpt-6-astra · high                                                    │
│ same as   #new codex@openai-sub:alex gpt-6-astra high                                                        │
│                                                                                                              │
│ ←→ column · ↑↓ choose · tab to account, effort, permissions · enter start it · esc cancel                    │
╰──────────────────────────────────────────────────────────────────────────────────────────────────────────────╯
```

Keeps from today: profiles row, account/effort/permission rows,
`openSetupSheet` for switching a session ("Switch to", in place or handed
over), `loadModels` off the UI, and the chip.

## 4. Settings, redrawn

Pages become **Providers · Harnesses · Profiles** · General · Appearance · Keys ·
Plugins · Updates. Models and Accounts are folded into Providers. Profiles and
folder rules get their own page (question 4).

### Settings › Providers

The left list holds providers only, grouped by how they're paid. On the right
is the selected provider: its accounts or key, then **the harnesses it can
run in** (★ the default, every ✓ launchable one-off), then the default
model, effort and mode for each route, then the provider's models.

```
 Settings                                                                                                  esc closes
──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
  Providers  │ Subscriptions                     │ ◎ OpenAI · subscription                   not the default provider
  Harnesses  │   ✻ Anthropic · sub  2  ▓▓▓░ 41%  │   ChatGPT Pro · alex · 5h 22% · 7d 48%
  Profiles   │ ▸ ◎ OpenAI · sub     1  ▓░░░ 22%  │
  General    │   ◈ GitHub Copilot   1  ▓▓░░ 30%  │ Accounts                            one at a time, all its sessions
  Appearance │   ■ Mistral          1            │   ● alex    alex@…        5h ▓▓░░ 22%   7d ▓▓▓▓░░ 48%
  Keys       │   ◐ Moonshot   not signed in      │   + add an account
  Plugins    │ API keys                          │
  Updates    │   ✻ Anthropic · API  ✓ key        │ Harnesses                 ★ new sessions start here · ✓ launchable
             │   ◎ OpenAI · API     ✗ no key     │   ★ ◎ Codex      0.130.0   gpt-6-astra   high   auto
             │   ◆ DeepSeek         ✓ key        │     – Claude Code, Pi, OpenCode: sub is Codex's only
             │   ▲ GLM (Z.ai)       ✓ key        │
             │ Local                             │ Models                                  context · runs in
             │   ◉ Ollama           3 models     │   gpt-6-astra   400k  the most capable  ◎ Codex ★
             │                                   │   gpt-6-sol     400k  the workhorse     ◎ Codex
             │                                   │   gpt-6-luna    200k  fast and cheap    ◎ Codex
──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
  enter open · a add account · $ API key · r read limits · * make default provider · tab next page
```

The same page with a provider that runs in several harnesses. The right pane
only:

```
             │                                   │ ◉ Ollama                  local · 3 models pulled · ollama 0.13.1
             │                                   │
             │                                   │ Harnesses                 ★ new sessions start here · ✓ launchable
             │                                   │   ★ ✻ Claude Code  2.1.30   qwen3-coder:30b  –   ask
             │                                   │   ✓ ◎ Codex        0.130.0  qwen3-coder:30b  –   auto
             │                                   │   ✓ π Pi           0.9.2    gpt-oss:20b      –   –
             │                                   │   ✓ ■ Vibe         2.0.1    (its default)    –   –
             │                                   │   ◌ ▣ OpenCode     not installed · possible later
             │                                   │   enter edits a row's defaults · * makes it ★
             │                                   │
             │                                   │ Models                                    context · runs in
             │                                   │   qwen3-coder:30b  256k  tools, fitted    ✻ ★ · ◎ · π · ■
             │                                   │   gpt-oss:20b      128k  tools            ✻ ★ · ◎ · π · ■
             │                                   │   qwen3-vl:30b     256k  images           ✻ ★ · π
             │                                   │   + pull a model · context size · keep loaded
```

Gone from this page: the feature grid (it moves to Harnesses), profiles,
folders, the "used / not used" toggle, and the nested Accounts page.

### Settings › Harnesses

The left list holds harnesses only, with version and support level. On the
right: install and update, its own sign-in where it has one, **the providers it
can run on** (★ where it's that provider's default, greyed with the reason
where it can't), what rush can do in it, and the harness's own settings.

```
 Settings                                                                                                  esc closes
──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
  Providers  │ ▸ ◎ Codex        0.130.0  tested  │ ◎ Codex                                   tested · by OpenAI
  Harnesses  │   ✻ Claude Code  2.1.30   full    │   ~/.codex/bin/codex · 0.130.0 · up to date         u updates
  Profiles   │   π Pi           0.9.2    tested  │
  General    │   ◈ Copilot CLI  1.0.4    tested  │ Runs on                       ★ this is the provider's default
  Appearance │   ■ Vibe         2.0.1    preview │   ★ ◎ OpenAI · sub    ✓ alex      gpt-6-astra  high  auto
  Keys       │   ◆ dsh          0.4.1    preview │   ★ ◎ OpenAI · API    ✗ no key: $ adds one in Providers
  Plugins    │   ✦ Antigravity  1.2.0    preview │     ◉ Ollama          ✓ 3 models  qwen3-coder  –     auto
  Updates    │   ▲ ZCode        –        preview │     ✻ Anthropic       – Codex speaks only OpenAI's API
             │   ◐ Kimi CLI     –        tested  │     ◆ DeepSeek · GLM  – the same
             │   ▣ OpenCode     –        preview │
             │                                   │ What rush can do in Codex
             │   –: not installed; i shows how   │   ✓ resume     ✓ images     ✓ effort     ✓ subagents   ◌ memory
             │                                   │   ✓ interrupt  ✓ modes      ✓ compact    – rewind      ◌ settings
             │                                   │   ✓ handoff in ✓ quota      ✓ accounts   – screen      …
             │                                   │
             │                                   │ Codex's own                 config.toml · AGENTS.md · ▸ Advanced
──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
  enter open · u update · i install · tab next page
```

A route's defaults (model, effort, mode) can be edited from either tab, and
both write the same `Starts["provider/harness"]`. There is no third place.

### Settings › Profiles

Today's profiles and folder-rules UI (`settings_profiles.go`), moved out of
the provider list as it is. A profile lists **routes** (`codex@openai-sub`),
which replaces its `Billing` and its per-provider `RunsIn`.

## 5. Migrations

| What | From | To | Notes |
|---|---|---|---|
| Start defaults | `Dispatch.Starts[kind]` plus legacy `Dispatch.Model/Effort/Permission` | unchanged, plus `Starts["claude-key"]` and `Starts["codex-key"]` | Built without a migration: every route but a split provider's key in its own harness is a kind of its own, so it stays keyed by kind. The key's entry falls back to the plan's until set (`state.Dispatch.StartOn`). |
| Harness use | `Config.Harnesses` (used / not used) | no longer shown | Left in the config. Every compatible, installed harness can be launched. |
| Default harness | `Config.RunsIn[providerId]` | unchanged | Already one default per provider. |
| Profiles | `Providers` + `Billing` + `RunsIn` + first-provider `Account/Model/Effort` | ◌ not yet: `Routes []string` (`codex-key/pi`) | Profiles read as before; their own page is built. |
| Kind names as profiles | `#profile ollama-pi`, folder rules naming a kind | still parsed, as a route | `ProfileNamed`'s kind branch (`profiles.go:173-180`) becomes a route lookup. |
| Typed names | `openai-codex:alex`, `codex:alex`, `claudecode:alex` | `codex@openai-sub:alex` | Old names stay as aliases (`pickSetup` already does this for `oldSetupName`). |
| `#with` | sets the default | `#new` with no task | Never changes a default. |
| Settings pages | Models, Harnesses, Accounts | Providers, Harnesses, Profiles | `openAgentSettings` goes to Providers; flash and error texts naming "Settings › Accounts" or "Settings › Models" are updated (`cross.go:112`, `settings_providers.go:926`, `docs/guide.md:106,118,125,147,465`). |
| Glyphs | `looks[kind]` | unchanged: a kind looks like its provider, a spinner like its harness | Anthropic in Pi now shows ✻, not ◇. |
| Sessions in flight | `host.Info.Kind` + `Billing` | unchanged | A route is derived (`ProviderOf`+billing, `HarnessOf`), so nothing is rewritten. |

## 6. Build order (once approved)

1. `agent.Compat(provider, harness) (kind, reason)`, provider and harness
   words, and aliases. Pure functions with a table test of the matrix above.
2. Re-key `Starts` by route, with the migration. State only.
3. `#new` parsing (shared with `/agent`), completion, and the border preview.
4. The start sheet's three columns, reading `Compat` reasons.
5. Settings: Providers (Models and Accounts folded in), Harnesses, Profiles.
   Delete `settings_models.go` and the Accounts alias.
6. Split the glyphs; update the guide.

Steps 1 and 2 can run in parallel; 3, 4 and 5 depend on 1. The in-flight
Settings redesign (untracked `settings_models.go`, `settings_harnesses.go` and
`settings_catalog_layout.go`) must land or be paused before step 5.

## 7. Open questions for Alex

**Rulings, 2026-10-01.** Q1: allow anything a harness can sign in to, with a
dim warning. Q2: allow, with a warning on the border. Q3: alias. Q4: own page.
Q5: words only. Q6: one key. Q7: as written. Q8: no. Q9: yes. Q10: fold into
`providers-harnesses.md` with a link. The questions as asked:

1. **Pi's and OpenCode's own logins.** Pi can sign in to a Claude or ChatGPT
   plan itself. Should rush allow only "Anthropic subscription → Claude Code"
   (as you said) and show a harness's own login as an opaque account under
   that harness? *Recommended: yes.*
2. **One-off accounts are global today.** Picking `:work` swaps Claude Code's
   sign-in for every session (`setup.go:286-304`). Is that acceptable with a
   warning in the preview, or should one-off accounts be per-session only
   (config-folder accounts, not swapped logins)? *Recommended: allow it, warn
   on the border.*
3. **`#with`** sets the default today. Should it be deleted, so Settings is
   the only place a default changes, or kept as an alias of `#new` with no
   task? *Recommended: alias.*
4. **Profiles and folders**: their own page, or kept at the bottom of
   Providers? *Recommended: their own page.*
5. **Provider ids.** Keep the stored ids (`claude`, `claude-key`, `codex` …)
   and change only what's shown and typed, or rename them with a migration?
   *Recommended: words only; no migration.*
6. **More than one API key per provider** (work and personal keys as
   accounts)? *Recommended: not now.*
7. **`#new pi task` with no provider.** Use the next session's provider when
   Pi can run it, else the first provider whose ★ is Pi, else any
   compatible one. Is that the right rule?
8. **A "save as default" key** on the start sheet? *Recommended: no;
   Settings only.*
9. **A model alone picks the provider** (`#new gpt-6-astra task`)? *Recommended: yes.*
10. **This doc versus `providers-harnesses.md`.** Fold this into it, or
    replace that file's Settings and Starting sections with a link here?
