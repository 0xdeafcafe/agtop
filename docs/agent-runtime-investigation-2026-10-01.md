# Agent runtime performance investigation — 2026-10-01

## Tasks

- [x] Trace launches and measure live process costs.
- [x] Check lifecycle, polling, plugins, and history handling for avoidable work.
- [x] Rank improvements by likely impact and implementation risk.

## Conclusion

There are substantial optimization opportunities, especially in tool subprocesses, idle lifecycle, and repeated initialization. The measurements do not establish a universal runtime speedup or memory reduction. Prioritize resource ownership and selective tool loading before replacing adapters or applying blanket heap limits.

This is an investigation, not an applied performance patch. No agent was stopped, no provider configuration was changed, and no inference request was initiated. Existing implementation work was left untouched.

## Measurements

Apple silicon machine with 10 logical CPUs and 64 GiB memory. Read the live process tree, took a five-second CPU-time delta sample, queried macOS process physical footprint, and sampled the busy gopls stack for two seconds. CPU percentages below use 100% per core. RSS includes shared mappings; physical footprint is a separate macOS accounting measure and can include compressed memory. Neither is Go allocation churn. These are different active workloads, not controlled provider comparisons.

| Process | RSS, MiB | Physical footprint, MiB | CPU over approximately 5s |
| --- | ---: | ---: | ---: |
| Claude, PID 67866 | 636 | 282 | 5.8% |
| Claude, PID 71439 | 763 | 297 | 1.4% |
| Codex, PID 79285 | 435 | 254 | 4.2% |
| Codex, PID 92780, this investigation | 245 | 85 | 0.4% |
| Kimi, PID 71696 | 416 | 283 | 0.4% |
| gopls under Claude, PID 25108 | 808 | 634 | 500.4% |
| Rush hosts for the measured Claude/Kimi sessions | 22–47 | 11–30 | 0–0.2% |

The two-second gopls sample independently reported 645.8 MiB physical footprint and a peak of 857 MiB. It contained repeated `golang.org/x/tools/internal/gopathwalk.(*walker).walk`, directory reads, path cleaning and file opens. Its working directory was the LangWatch repository. This identifies expensive directory traversal, not a proven root cause for why that traversal repeats. Raw sample: `/tmp/rush-gopls-investigation.sample.txt`.

Each of the two Claude sessions had three Playwright MCP server processes plus one npm launcher. Their combined measured physical footprint was approximately 277–280 MiB per session before any browser they might launch. One server used `--headless`; the others did not. Audit their roles before deduplicating: equal executable paths do not establish equal capabilities or session state.

Other development tools were substantial: two Node processes outside the surviving Rush host ancestry had footprints around 1.3 and 1.5 GiB; active test workers, browsers and Nx daemons added more. Parent PID 1 alone does not prove an orphan is disposable. The machine had about 600 MiB swap used at the initial snapshot; that does not establish current swapping or memory-pressure severity.

## What Rush already does

- Claude uses headless stream JSON; Codex uses `app-server`; Kimi/OpenCode/Gemini/Vibe use ACP; Pi uses RPC. These paths already avoid running the CLI terminal interfaces.
- Hosted sessions default to stopping their runtime three seconds after a completed turn. Background tasks, outstanding control requests and certain shell descendants postpone that stop. See `internal/host/host.go:277`, `armIdle`, `stillWorking` and `runsShells`.
- Process groups and a host-death guard already exist. Rush also exposes orphan cleanup and hibernation settings. Improve these mechanisms rather than introduce competing cleanup loops.
- Claude can skip decoding relayed deltas/tool results via `Lightly`; Codex resume/fork requests already use `excludeTurns`. Rush hosts already tune Go GC and their memory limit.
- Ollama is a provider behind Claude, Codex or Pi adapters here. Local model residency is an additional cost; choosing Ollama does not remove the selected harness's overhead. Its observed server RSS was about 42 MiB, with no model runner visible in the initial snapshot.
- No representative live Gemini, Vibe, OpenCode or Pi session was measured. Their adapters were inspected, but they cannot be ranked by memory from this sample.

## Prioritized improvements

### 1. Control language-server and browser-tool duplication

**Highest measured CPU opportunity.** The gopls process consumed about five cores while its Claude host reported the main turn idle with background work outstanding. Make language-server loading dependent on project/task needs; diagnose the import-directory scan and constrain its scope. Use per-server concurrency settings only after checking indexing latency. Directory filters and a narrower workspace may help, but this stack sample does not prove they address the particular import scan. The obsolete `memoryMode` setting has no effect. [Official gopls settings](https://go.dev/gopls/settings).

For Claude, inventory MCP servers by configuration origin, executable and relevant options; avoid launching unused browser integrations and eliminate genuinely redundant configurations. The measured three-server bundles make hundreds of MiB per active session worth investigating, but the achievable saving depends on which capabilities must remain.

Current `Without` maps to `--disallowedTools` in `internal/adapters/claude/conn.go`; hiding tools from model context should not be treated as proof their server processes never start. Claude supports an explicit MCP selection with `--mcp-config` and `--strict-mcp-config`. `--bare` also skips instructions, skills, hooks, plugins and memory, so it is unsuitable as a transparent default for existing sessions. [Official CLI reference](https://code.claude.com/docs/en/cli-reference).

### 2. Make idle shutdown observable and close lifecycle gaps

**Direct memory opportunity; correctness-sensitive.** Kimi host `be8f5917` reported idle, no background tasks, no queue, and a three-second timeout, yet its approximately 283 MiB-footprint runtime remained alive over successive observations. It was an older protocol-7 host; current source is protocol 8. This is an anomaly to reproduce, not proof of a specific current-source bug. Pending internal asks and timers are not exposed in saved metadata.

Add an explicit reason for staying resident: active turn, approval, background task, pending control request, grace period, or shutdown in progress. Validate liveness against process identity rather than trusting old info.json records.

There is also a current-source path worth a focused regression: `Run` can start a harness solely to accept carried history, while `event.Init` does not arm idle shutdown. Ensure successfully initialized, otherwise quiet carry-only sessions eventually rest. Preserve pending work and the handoff acknowledgement before doing so.

Prefer per-harness warm grace periods with pressure-driven eviction over assuming every runtime is cheap to restart. Shorter grace saves resident memory; longer grace avoids repeated plugin/MCP startup. Measure both.

### 3. Resume ACP sessions without replay when supported

**Concrete adapter optimization.** `internal/adapters/acp/session.go`, `begin`, prefers `session/load` whenever `loadSession` is available, even if `session/resume` is also advertised. Kimi documents `session/resume` as restoring without history replay. [Official Kimi ACP reference](https://www.kimi.com/code/docs/en/kimi-code-cli/reference/kimi-acp).

Use capability-negotiated lightweight resume for host restarts where Rush already owns the history; retain load for initial history acquisition or incompatible implementations. This needs tests for visible history, duplicate events, tool state and resumption failures. Do not globally substitute the methods without those ownership checks. Saving is proportional to history replay avoided; no before/after timing was measured.

### 4. Budget concurrent work across sessions

`internal/agent/subagents.go` returns `max(20, runtime.NumCPU()*4)`: 40 on this machine. `internal/adapters/claude/conn.go` passes that as Claude's cap when no environment override exists. This is a per-session upper bound, not a claim that 40 processes were running; native agents can share a runtime.

Introduce a configurable global budget for spawned runtimes and heavy tools, with explicit queueing. Start by evaluating a small heavy-tool budget, then tune against actual CPU/memory pressure. Rush's optional gate already provides cross-session command queues (`internal/gate`, `internal/bundled/gate`); extend its coverage and measurements. One admitted test command may create many workers, so command count alone does not bound total resource use. Preserve an escape path for interactive/lightweight work and avoid nested queue deadlocks.

### 5. Consider shared servers only after lifecycle fixes

Rush currently spawns one Codex app-server per connection (`internal/adapters/codex/rpc.go`). Pooling by compatible account/configuration could amortize process startup and some shared resources, but requires thread-based event routing, approval isolation, cancellation and crash recovery. It does not guarantee MCP resources are shared.

Codex documents `thread/unsubscribe`, but the current documentation describes a 30-minute inactive grace before the last-unsubscribed thread unloads. Pooling without verifying the installed version's unload behavior could retain more history than the current three-second process shutdown. Treat pooling as an experiment, not the first fix. [Official app-server lifecycle](https://developers.openai.com/codex/app-server).

### 6. Bound transport backlog and profile startup separately

Codex, ACP and Pi have event queues that append without a byte cap. A slow consumer can retain an increasing backlog; this was not observed as the cause of the live agent costs. Coalesce compatible text deltas and establish a byte budget or spool strategy without dropping approvals, tool completion or turn boundaries. Do not simply block the RPC reader if that prevents responses needed to unblock the host.

Claude's Quick start option exists but was off for the measured sessions. Its code comment's historical startup timings were not remeasured. Benchmark current initialization with/without that option and selected plugins. It is not a general memory control, nor is a lower model effort a local-process memory fix.

## Implementation order and acceptance evidence

1. Add resource breakdown and residency reasons; reproduce idle Kimi and carry-only initialization; fix confirmed lifecycle cases with fake-harness regression tests.
2. Audit the three browser-server roles and gopls traversal, then implement per-session tool selection. Measure identical tasks with identical required capabilities.
3. Implement capability-aware ACP lightweight resume and compare a fixed long session across repeated rests/wakes.
4. Evaluate global concurrency budgeting and only then prototype pooling.

For each change record process-tree physical footprint, CPU seconds, startup-to-ready and first-token latency, idle reclamation time, history integrity, and approval/background-task behavior. Include native subprocesses and separately attribute detached tools. Run matched workloads repeatedly; existing Go allocation benchmarks cannot establish these runtime savings.

Validation in this pass: live resource sampling, parent/child attribution, filtered metadata/configuration inspection, stack sampling, adapter/lifecycle source review and official protocol documentation. No implementation tests or authenticated agent smoke tests were run because this pass changed only this report.

## Configuration and build follow-up tasks

- [x] Identify installed runtimes and supported tuning controls.
- [x] Assess headless configuration, memory limits, and rebuild options.
- [x] Document concrete presets and their tradeoffs.

## Configuration, environment and rebuild findings

The installed versions inspected were Claude 2.1.284, Codex 0.155.1, Kimi Code 2.0.1, OpenCode 1.18.30 (Homebrew path), and Pi 0.99.2. Claude, Codex and OpenCode are native arm64 Mach-O files. Claude/OpenCode contain Bun/JavaScriptCore runtime markers. Kimi's standalone executable contains Node/V8 markers, not the Bun or Python markers checked. Pi is a Node bundle requiring Node >=22.19.0. Therefore old Python Kimi advice and blanket NODE_OPTIONS settings for Claude would target the wrong runtimes.

### Claude: a bounded headless profile

Candidate per-session environment, with values to benchmark rather than claimed optimal defaults:

```sh
CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1
CLAUDE_CODE_MAX_CONCURRENT_SUBAGENTS=4
CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH=1
CLAUDE_CODE_MAX_TOOL_USE_CONCURRENCY=4
```

The first setting disables updates and several background/network features; use it where Rush handles updates and the lost integrations are acceptable. The remaining settings reduce concurrent agents, nesting and tool parallelism. Lower caps can lengthen highly parallel tasks. Rush already maps its Quick start option to the first variable. [Official environment reference](https://code.claude.com/docs/en/env-vars).

Use per-session `--settings` overrides for unneeded installed plugins and explicit MCP selection. Keep the instructions, permissions, hooks and session persistence required by the task. The installed `--bare` help specifically says OAuth/keychain authentication is not read: Anthropic authentication must use ANTHROPIC_API_KEY or apiKeyHelper. It also disables substantial context and integrations. This makes it inappropriate as a default for current subscription sessions. `--no-session-persistence` conflicts with Rush's rest/resume design. The existing stream-JSON flags already avoid TUI rendering.

### Bun: low-memory mode is a tradeoff, not free acceleration

Bun documents runtime flags for compiled applications through `BUN_OPTIONS`; `--smol` collects more often and grows its heap more slowly at a performance cost. Candidate: `BUN_OPTIONS=--smol` for a scoped Claude/OpenCode experiment, preserving any existing options. [Compiled executable options](https://bun.sh/docs/bundler/executables), [smol semantics](https://bun.com/docs/runtime).

Three interleaved `--help` samples per mode, measured with macOS `/usr/bin/time -l`:

| Program | Default peak RSS MiB | smol peak RSS MiB | Default elapsed s | smol elapsed s |
| --- | --- | --- | --- | --- |
| Claude | 107.0 / 106.8 / 106.9 | 107.4 / 104.7 / 107.2 | .114 / .093 / .103 | .122 / .140 / .323 |
| OpenCode | 146.7 / 141.2 / 141.5 | 139.6 / 142.7 / 141.9 | 1.230 / .844 / .873 | 1.298 / .986 / 1.478 |

All exited successfully. No consistent memory benefit appeared in these short launches; timing was generally worse. This is only launch compatibility and help-path evidence, not a long-session benchmark or proof the application cannot override the runtime policy. Do not enable globally based on this test.

### Codex: control subsystems, not V8

Candidate overrides for `codex app-server`:

```sh
-c agents.max_concurrent_threads_per_session=4
-c agents.max_depth=1
-c mcp_servers.UNNEEDED_SERVER.enabled=false
```

Replace the MCP name with an actual optional server. Current documentation supports these settings, and the installed binary contains both current and legacy concurrency-key names. App-server supports `--strict-config` to catch unknown fields. Analytics are already off by default according to the installed app-server help. `NODE_OPTIONS`, Go GC knobs and Bun flags do not configure the Rust runtime. [Official configuration reference](https://developers.openai.com/codex/config-reference/).

### Kimi, Pi and other harnesses

Kimi candidate env: `KIMI_CODE_NO_AUTO_UPDATE=1` when updates are managed centrally; optionally `KIMI_DISABLE_TELEMETRY=1`. These remove update/reporting activity, not the main heap. [Official Kimi environment reference](https://www.kimi.com/code/docs/en/kimi-code-cli/configuration/env-vars).

Kimi and Pi can be evaluated with `NODE_OPTIONS=--max-old-space-size=768`. Kimi's help launch exited successfully with this option, but enforcement and a useful budget still require a real session test. This bounds V8 old space, not RSS, and can increase GC or cause allocation failure. `NODE_COMPILE_CACHE` can reduce repeated module compilation for ordinary Node workloads; don't assume it benefits an already snapshotted/bundled executable. Scope options to the intended processes rather than every descendant. [Node options](https://nodejs.org/api/cli.html).

Pi's installed help supports `--offline` / `PI_OFFLINE=1`, `--no-extensions`, and explicit `-e` extensions. Rush already skips Pi version checks and maps Lean to PI_OFFLINE. An explicit extension set is a useful experiment if required Rush tools are retained. Avoid `--no-context-files` as a transparent optimization because it removes AGENTS.md/CLAUDE.md discovery.

OpenCode's installed CLI supports `--pure` for no external plugins. Its config supports `watcher.ignore` for generated/dependency directories, `autoupdate:false`, and selective LSP disabling. Disabling LSP removes diagnostics/navigation capabilities; disabling snapshots changes recovery behavior. ACP already avoids the TUI. [Config](https://opencode.ai/docs/config/), [LSP configuration](https://opencode.ai/docs/lsp/).

Gemini and Vibe already use ACP in Rush. No validated additional low-memory profile was established for their installed versions in this follow-up; do not copy Bun/Node/Python options across harnesses without identifying the executable and measuring it.

### gopls and Ollama budgets

For the measured gopls CPU problem, an initial *server-specific* experiment is `GOMAXPROCS=2`, with workspace/import-scan tuning. A separate memory experiment could use `GOMEMLIMIT=1GiB`; it is a soft runtime budget, not an RSS cap. Avoid lowering GOGC aggressively while trying to reduce CPU, because that increases collection frequency. Rush's own hosts already use a 48 MiB soft Go memory limit and GOGC 25 by default. [Go GC guide](https://go.dev/doc/gc-guide).

For Ollama, set server variables on the actual server process/service, not just the invoking agent:

```sh
OLLAMA_MAX_LOADED_MODELS=1
OLLAMA_NUM_PARALLEL=1
OLLAMA_KEEP_ALIVE=2m
```

Also evaluate a smaller task-appropriate context, Flash Attention where supported, and `OLLAMA_KV_CACHE_TYPE=q8_0`. The quantized KV cache can use about half the f16 cache memory, not half of total model memory, with a precision tradeoff. Short keep-alive frees models sooner but adds reload latency; request-level settings can override defaults. [Official Ollama FAQ](https://docs.ollama.com/faq).

### Recompiling: targeted experiments only

- **Codex:** benchmark a source release build with thin LTO and fewer codegen units against the vendor build. Release mode already enables optimization; the vendor's precise build settings were not inspected. For a source checkout, `CARGO_PROFILE_RELEASE_LTO=thin CARGO_PROFILE_RELEASE_CODEGEN_UNITS=1 cargo build --release` is a candidate, subject to the project's own build instructions. Host-specific CPU targeting can sacrifice portability. LTO does not remove retained histories or child processes. [Cargo profiles](https://doc.rust-lang.org/cargo/reference/profiles.html).
- **OpenCode / source-available Bun CLIs:** inspect existing build flags first; if absent, evaluate compiled bytecode, minification and profile-guided bytecode layout. These target parsing and startup; rebuilding a bundle does not eliminate its JavaScript heap. Bun documents these mechanisms, but its published example improvements are not measurements of Rush or these agents. [Bun executable builds](https://bun.sh/docs/bundler/executables).
- **Rush / gopls:** profile-guided Go builds are possible (`go build -pgo=profile.pprof ...`). A representative CPU profile matters more than a generic optimized-build recipe. Go's usual build already optimizes. Stripping symbols mainly reduces the binary's disk size. [Go PGO](https://go.dev/doc/pgo).
- **Claude / installed Kimi:** no supported matching source-rebuild path was established for these distributed builds. Don't assume the legacy Python kimi-cli source produces the installed Kimi Code 2.0.1 runtime. Configuration and measured runtime controls are the practical immediate path.

Recommended product shape: per-harness launch settings separating startup work, tool availability, concurrency and memory budget. Keep a normal profile and explicit reduced-resource experiments. Start with unnecessary plugin/MCP loading and concurrency; test memory GC modes separately so regressions can be attributed.

No persistent runtime/configuration changes or rebuilds were applied. Tests in this follow-up were help/version commands, binary inspection and the help-only comparison above; no paid inference calls were made.
