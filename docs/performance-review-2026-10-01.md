# Rush performance and multi-model review — 2026-10-01

Reviewed the working tree on an Apple M1 Max, Go 1.27.1, darwin/arm64. The tree already contained other work; these measurements describe that tree and the changes made during this review, not a clean release comparison. Allocation figures are bytes allocated per operation, not retained memory or peak RSS. Concurrent builds affected timing; allocation counts were more stable.

## Measurements

| Workload | Initial review | After the handoff changes |
| --- | --- | --- |
| Complete conversation export, 3,000 turns | 9.79 ms; 30.7 MB; 72,187 allocations | Approximately 0.8–1.7 ms; 485 KB; 6,606 allocations |
| Warm conversation frame, 3,000 turns | 1.13 ms; 266 KB; 1,747 allocations | 1.5–3.0 ms in the final profiled run; 270 KB; 1,780 allocations |
| Agent list, 30 / 300 agents | 0.33 / 1.17 ms; 162 / 654 KB | Not optimized in this change |
| New header colour wash, one row | Not present | About 2–3 μs; 1.2–2.8 KB; 2–3 allocations in its focused benchmark |

Warm-frame wall time varied substantially, so this review does not claim a general frame-latency improvement. Frame allocations increased by roughly 4 KB with the added UI. The final CPU profile still identifies historical turn lookup/bookkeeping (about 26% cumulatively) and cell-width calculation as significant costs; the new header wash is not a dominant hotspot.

### Account-summary follow-up

A matched `BenchmarkLiveFrame/conversation/3000turns/tick` run (three samples each, same fixture and geometry) measured:

| Metric | Immediately before frame-local account caching | After |
| --- | --- | --- |
| Frame wall time | 2.00 / 2.62 / 2.77 ms | 1.88 / 1.93 / 2.76 ms |
| Bytes allocated per frame | About 300.8 KB | About 260.5 KB |
| Allocations per frame | 2,642–2,643 | 2,571 |

This removes about 40 KB of allocation per frame (13.4%) and about 72 allocations (2.7%). Timing ranges overlap under concurrent development/builds; this is evidence of lower allocation churn, not a reliable frame-latency guarantee. The immediately-before fixture included subsequent UI work and therefore differs from the earlier 270 KB / 1,780-allocation profile above.

`accountRows`, `inUseRows`, and low-quota notes now share results only while `View` renders a frame. All cached references are cleared before the next update. Accounts without a low, fresh quota no longer construct a list of alternative accounts. A regression renders two actual frames around an account/configuration update and checks that the new account, name and quota appear immediately; command handlers outside rendering still read fresh rows.

The complete export is now bounded before allocating its step summary. It retains the latest 64 calls, reports the exact number of older calls omitted, retains all changed paths, and keeps recent conversation history under its existing 200 KB budget. Oversized messages retain their beginning and end without splitting UTF-8. Mid-turn instructions survive the handoff.

The export still runs on the UI goroutine for an open session, which owns that mutable conversation. Its measured cost is much lower; moving it off-thread would require an immutable snapshot or another explicit ownership boundary.

## Remaining performance findings

1. `internal/convo/render.go`: `RenderInto` still visits cached historical turns. Cached text avoids repeated layout, but warm-frame bookkeeping grows with history length. A viewport index is the next useful optimization; retain stable selection, folded groups, and scroll anchors when changing it.
2. `internal/ui/view.go`: the agent list formats rows beyond the viewport. Work and allocations grow with the number of agents. Compute visible rows first, retaining lightweight geometry for navigation.
3. Large transcript opening still allocates heavily: the initial 18–19 MB samples allocated about 55–76 MiB, retaining about 22 MiB. Warm scrolling was much cheaper, around 0.4 ms. Cold open/layout and warm rendering must be profiled separately.

The initially reported 45–96 ms stalls were not consistently reproduced. One cold overview took about 24 ms and a Codex layout sample about 254 ms; those are observations, not stable latency guarantees.

## Changes that protect responsiveness

- Account rows, active-account summaries and low-quota notes are shared within each frame and discarded afterward. The earlier allocation profile attributed about 18% cumulatively to `lowNote` and about 13% to `accountRows` (overlapping totals); the matched benchmark above measures the resulting whole-frame reduction.

- Kimi, Vibe and Gemini model catalogs are read asynchronously; `Choices()` no longer reads configuration files during rendering.
- Image decoding and file reads for the full-message viewer run off the UI goroutine. Wrapped message text is cached by message and width.
- Header colours use a bounded palette cache and a static gradient, with no animation loop.
- Shared discussion rounds are explicitly triggered and bounded to one response per participant. Stream decoding is batched off the UI thread.
- Ollama discovery does not load a model. The local server starts only if necessary, and the chosen model loads when a session needs it.

## Correctness fixes affecting the experience

- Host sent-message echoes now merge with provider acknowledgements instead of becoming duplicate mid-turn inputs. Image-only and mixed text/image messages remain user messages.
- Sent pastes and images open a full-message viewer; first/latest navigation preserves drafts.
- `/model` and `#model` open a picker even when the catalog is initially empty or the conversation is a native transcript. Native sessions explain how to move into Rush before changing runtime settings.
- Shift+Tab opens session model/effort/harness controls. F1 shows current/remapped shortcuts.
- Cross-harness switching preserves the source until destination initialization succeeds. Busy or queued sources are not silently discarded. Ollama model switches restart with model-specific tuning.
- Kimi, Vibe and Gemini have native history/model/configuration support, replayed turn boundaries, and native login entry points. Capabilities remain explicit; a saved-history reader does not imply external live-process tracking or native cross-provider import.
- Vibe's model catalog includes built-in defaults, not just optional TOML overrides. Account information comes from its matching native account cache, without exposing credentials.
- Models are grouped by provider; Harnesses are listed separately. Details describe the selected pairing rather than implying every model works in every harness.

## Reproduce

```sh
TMPDIR=/tmp go test ./...
TMPDIR=/tmp go test ./internal/convo -run '^$' -bench BenchmarkConversationExport -benchmem -count=3
TMPDIR=/tmp go test ./internal/ui -run '^$' -bench 'BenchmarkLiveFrame|BenchmarkListMany|BenchmarkHeaderWash' -benchmem
TMPDIR=/tmp go test ./internal/ui -run '^$' -bench '^BenchmarkLiveFrame/conversation/3000turns/tick$' -benchmem -count=3
TMPDIR=/tmp go test ./internal/ui -run '^$' -bench BenchmarkLiveFrame -cpuprofile /tmp/rush-ui.cpu -memprofile /tmp/rush-ui.mem -o /tmp/rush-profile-ui.test
```

Compare the same fixture, width, view, toolchain and machine. CPU profiles and allocation profiles answer different questions: use `alloc_space` for churn and `inuse_space` for retained memory. Real authenticated provider conversations require separate smoke tests; parser, fake-host and race tests do not establish service-side compatibility.


## Verification completed

- Account-frame freshness, account controls and quota-note regression tests passed after the frame-local cache change.

- `go test ./...` passed across the repository.
- Race checks passed for conversation handling and the Ollama, ACP, Vibe and Gemini adapters.
- Focused UI race checks passed for the message viewer, account information, model commands, draft-preserving controls and handoff readiness.
- Discussion race tests cover real local Unix-socket host transport, bounded rounds, shared context, cancellation, replay termination, persisted archives, and actual model labels, without making paid provider requests.
- Carried-history startup race tests passed for the host and Claude adapter.

Live authenticated provider requests were not run. Native transcript sessions expose the model picker, but must be moved into Rush before Rush can control their model.

## Background-view follow-up

Expanded tasks now show recent output before process details and the launch script. The script is generated only when command details are explicitly opened with `d`. Task descriptions take precedence over raw commands; process rows show executable names by default. Output activity uses the existing asynchronously read metadata, without a filesystem operation in rendering. Quiet output is described as a possible wait, not proof of a hung process. Completed-task cache keys now compare exact summary fields without formatting a temporary string, also fixing stale equal-length summaries.

One post-change sample of the existing real-session `BenchmarkBackgroundView` fixture measured 2.30 ms / 255 KB / 2,748 allocations per frame; keyboard navigation plus rendering measured 1.44 ms / 237 KB / 1,959 allocations. These are current-cost observations, not a matched before/after speed claim. The fixture depends on local session history.

## Exchange history and controls

Rush-managed `session send` and spawned-agent requests/results now retain correlated sender and receiver identities on both sides. Guidance semantics are preserved; failed sends do not create success records. Queued messages retain their origin through editing and moving; different senders cannot be merged into an unattributed message. UI rows use Space to fold, Right to visit the peer, and Alt+C to copy the full exchange. Enter sends only while the composer owns focus.

Exchange journals survive host replacement and restore with provider history on background readers. Each session journal is capped at 8 MiB, with at most the latest 2,048 complete records restored. Individual exchanges larger than 8 MiB are not journaled. Provider-native internal agents outside Rush's managed sends/spawns remain dependent on the provider's own transcript support. Existing hosts older than protocol 8 keep delivering messages and explain that attribution needs a host restart.

Every agent-list row names its harness, including Claude Code and Kimi. Long titles reserve space for the identity badge; an attached host's identity takes precedence over stale fleet metadata.

Final follow-up verification: the full repository test suite passed, as did focused race checks for exchange transport, journal restoration, queue attribution, row controls, background diagnostics and frame-cache freshness. The stream batching test uses simulated time to avoid scheduler-load flakes while retaining its latency assertions. The resulting binary was built and installed locally.


## Stable loading and quieter UI follow-up

Hosted Codex reopening used the full pre-replay history path, bypassing its bounded tail reader. It could also resolve a known transcript by searching dated directories again. The UI then published pre-replay content before the current replay was complete, producing a visible older-to-newer jump. Scrolling across a long prompt could snap backward to its heading. Closing a large conversation additionally scheduled a forced full garbage collection, competing with the next opening.

The hosted path now reads a bounded tail before the replay cutoff, reuses the parsed session, and publishes the assembled current conversation in one handoff. Older history is hydrated afterward with turn references and scroll anchors preserved. Async results carry their connection identity so a late result cannot overwrite a reopened session with the same key. Long-prompt scrolling no longer rewinds to its heading; normal garbage collection replaces the forced collection on close.

Controlled first-visible-frame benchmark, 4,096 synthetic Codex turns (about 40 MB), same known path and replay, 160×40 viewport, three iterations on this M1 Max:

| History reader | First frame | Allocated bytes | Allocations |
| --- | ---: | ---: | ---: |
| Whole pre-replay prefix | 614.7 ms | 160,993,986 | 153,281 |
| Bounded cutoff tail | 83.6 ms | 12,683,722 | 11,981 |

This isolates history preparation, replay assembly and rendering. It excludes fixture generation, provider startup/network and subsequent full-history hydration. The machine was heavily contended (load average above 80), so timing is illustrative; allocated bytes fell about 92%. It is not a promise that every real conversation opens in 84 ms. A metadata-only inventory found a largest Codex transcript of 303 MiB, making the old unbounded path particularly expensive.

Adapter-only synthetic cutoff reading measured roughly 136 ms / 34.5 MB allocated for the full prefix versus 13 ms / 2.45 MB for the bounded tail. Kimi/Vibe JSONL tails also avoid whole-file reads for stopped/external history. Hosted timestamp-less Kimi records still cannot safely express a replay cutoff; they retain the correctness-first fallback. Gemini logs contain rewrites and upserts, requiring reduction of the whole history; removing duplicate decoding saves about 12% of allocations but did not establish a latency improvement. Full-history hydration still allocates the full conversation.

The default status strip now keeps spend, named provider usage and agent RAM visible, with exceptional system alerts. Detailed custom layouts remain intact. Elapsed quota windows retain the last measured usage and say refresh is needed rather than inventing renewed allowance. The usage-limit prompt supports passive `r` refresh and shows usage, reset countdown, last successful check and refresh errors without automatically continuing a session.

The header uses static, deterministic blocky patches only in its final 30%, moving from harness to model-provider colour and fading back to the normal edge. The first 70% stays unchanged. See `docs/header-preview.txt`. Cached allocation counts stayed unchanged, with allocated bytes falling about 40%; no timing improvement is claimed.

Regression checks cover incomplete replay staying off screen, stale connection results, full-history replacement, anchored scroll positions and long prompts. These are state/render tests, not proof that every terminal compositor is flicker-free. Bubble Tea already negotiates synchronized terminal output.

Reproduce:

```sh
TMPDIR=/tmp go test ./internal/ui -run '^$' -bench '^BenchmarkHostedCodexFirstFrame$' -benchmem -benchtime=3x
TMPDIR=/tmp go test ./internal/ui -run '^$' -bench '^BenchmarkView$' -benchmem -benchtime=100ms
TMPDIR=/tmp go test -race ./internal/ui -run 'Test(IncompleteReplay|ReopenedSession|ScrollThrough|Whole|HostedOpens|ScrolledUp|TakeReplay)' -count=1
```

The Keys page follow-up replaces the oversized keyboard diagram with an action/binding table. Selected descriptions and all binding alternatives wrap in full; editing controls remain visible down to 44×24. Hidden animation interception no longer consumes letters while recording chords. Responsive layout, mouse-row mapping, binding capture/practice and invalid-binding explanations have focused coverage. A conflicting history-close default was moved to `Ctrl+] s`.

Final stable UI verification: full repository tests passed after the final Keys edits; focused loading/scrolling race checks and adapter/header/usage-limit checks passed. Build installed atomically at `/Users/lw/go/bin/rush`. Existing sessions were not restarted. No authenticated provider requests or live terminal compositor verification were performed.


## Live CPU spikes follow-up

The live Rush UI (PID 73731) was observed at 83.9% CPU. An eight-second macOS `sample` capture showed substantial recursive `fleet.bulkUsage` work inside filesystem calls. Its open directories matched the large worktree/dependency tree in the user's report. Disk metering was infrequent but unpaced: a single refresh could still consume a core. The sample is `/tmp/rush-live-cpu-73731.txt`.

Automatic temp and worktree measurements now share one serialized walker budget: roughly 2 ms of work followed by 18 ms of rest. Foreground cleanup measurements retain their existing path. No files are excluded, symlinks remain unfollowed and allocated-block totals are unchanged. This reduces bursts at the cost of slower background size refreshes; it is not a hard limit on total process CPU or on individual filesystem calls.

Controlled benchmark on a fixed 2,000-directory tree, three walks per path, fixture creation excluded:

| Disk walk | CPU / wall time | Wall time per walk |
| --- | ---: | ---: |
| Foreground, unpaced | 92.99% of one core | 65.2 ms |
| Background, paced | 10.69% of one core | 771.1 ms |

The exact-size test also covers nested files, multiple bulk-read batches, symlinks and missing roots. Reproduce with `TMPDIR=/tmp go test ./internal/fleet -run '^TestDirUsageMatchesStat$' -bench '^BenchmarkDiskUsagePacing$' -benchtime=3x -count=1`. Raw results: `/tmp/rush-disk-pacing-benchmark.log`.

Separately, stopped/sleeping hosted rows now reuse unchanged transcript, git and subagent metadata for up to 15 seconds. Host state changes invalidate immediately; names/groups/spend overlays still update on every load. Active agents retain their existing refresh behavior. A 15-second real-data headless soak at 240×65 with session 7042a2fc selected changed from 12.31% CPU / 19.8 MB allocated per second to 10.99% / 17.9 MB per second. This was a modest improvement, not proof that the live spikes were solved: live data changed between runs and the disk scans were not due during the soak. Profiles: `/tmp/rush-live-before.cpu`, `/tmp/rush-live-after.cpu`, with matching `.alloc` and `.txt` files.

The full regression run also exposed a separate startup stall: constructing another agent's runner instructions synchronously invoked Antigravity model discovery while holding the host lock. A disposable test-host stack confirmed that path. Startup now reads a local, expiring model/availability snapshot; explicit account/model refresh retains discovery. Unverified availability is reported as unverified. The four previously failing CLI startup tests then passed, and the complete CLI package passed.

Final verification: `TMPDIR=/tmp go test ./...` passed, including the full UI suite (62.190 s) and CLI suite (4.364 s). Focused race checks for stopped-row caching, disk size consistency and Antigravity catalog freshness passed. Earlier focused Codex compaction/exchange/UI race checks passed. `git diff --check` passed.

The final real-session soak measured **15.66% CPU and 24.7 MB allocated/s**, with **233 writes / 67 KB** versus the baseline's 65 writes / 19.3 KB. The live session was substantially more active; these runs do not demonstrate an overall steady-state reduction. The controlled disk-scanner comparison above is the evidence for reduced scan bursts. Further live sampling after UI reload is needed to establish residual peaks; other rendering, polling and filesystem work can still consume CPU.

Built and atomically installed `/Users/lw/go/bin/rush`; previous binary saved at `/tmp/rush-before-cpu-nal23hbv`. Active sessions were not interrupted. The currently running UI must use `#reload` to adopt the fix. No claim is made that the old running process's CPU changed on installation. Final profile artifacts are `/tmp/rush-live-final.cpu`, `/tmp/rush-live-final.alloc` and `/tmp/rush-live-final.txt`.
