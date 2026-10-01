# Rush task list

## Replace Gemini CLI with Antigrav

- [x] Verify the Antigrav CLI command, protocol and migration path.
- [x] Update the harness integration, settings and compatibility.
- [x] Verify launch/discovery behavior and install.

Installed official checksum-verified agy 1.2.14 and Rush integration; native sign-in is still required. Adapter/race, UI, host, harnessup and CLI suites pass. Logs: `/tmp/rush-antigravity-full.log` (adapter suites), `/tmp/rush-antigravity-final.log`, `/tmp/rush-antigravity-race-final.log`. Binary backup: /tmp/rush-before-antigravity-tenu3zr9. Live signed-in inference remains unverified; integration is labeled preview.

## Queued messages survive restarts

- [x] Inspect queue persistence and notify the Ollama agent about the unreadable notice.
- [x] Fix restart recovery and add focused coverage.
- [x] Run checks and install.

Validation: host/UI suites pass; actual restart, crash, automatic queue delivery and focused race tests pass. Installed; backup /tmp/rush-before-queue-recovery-vki9x5vg. Ollama agent c83ee21b notified about the unreadable working notice.

## Quieter agent header and readable usage

- [x] Simplify header usage and identity, keeping details accessible.
- [x] Verify compact layouts and context click targets, then install.

Validation: UI and statusline suites pass (`/tmp/rush-quiet-agent-final.log`); built and installed. Binary backup: /tmp/rush-before-quiet-agent-gv6r5plm. Bars backup: /tmp/rush-bars-before-quiet-agent-ayg1goya.

## Page top spacing and icon lid

- [x] Lift page content one row while preserving icon position, and brighten its lid.
- [x] Verify header sizing and click targets, then install.

## Chunkier header colour

- [x] Extend static patches across the header height with larger blocks.
- [x] Verify colour, contrast, tab styling and install.

## Move header padding above title

- [x] Locate the blank row below the tabs and its hit targets.
- [x] Move the padding to the top and align mouse targets.
- [x] Verify and install.

## Header colour restoration and pattern preview

- [x] Find why header colour disappeared.
- [x] Restore the static colour pattern across 60–100% and prepare pattern sketches.
- [x] Verify header rendering and install.

## Interaction feedback, pane spacing and location clarity

- [x] Inspect hover/click targets, screenshot spacing, shell diagnostics and repository/worktree identity.
- [x] Add consistent interaction feedback and tighten header/banner geometry.
- [x] Bring existing shell diagnostics into Rush and stabilize location labels.
- [x] Verify interactions, rendering and diagnostics, then build and install.

## Compact status and idle lifecycle

- [x] Inspect status composition and agent/host idle exit and restart behavior.
- [x] Condense repeated usage into a compact status strip with details accessible.
- [x] Ensure idle agent processes and their Rush hosts exit safely and restart on demand.
- [x] Verify lifecycle and layout regressions, build and install.

## Community help and consistent permission controls

- [x] Inspect existing discussion, session commands and permission picker contracts.
- [x] Add a persistent shared help board: agent questions, replies and resolution, visible inside Rush.
- [x] Add permission overrides to Shift+Tab and matching discoverable commands with consistent harness support.
- [x] Verify integration and permission behavior, document controls, build and install.

## Current pass — stable conversations and quieter UI

- [x] Profile conversation opening and scrolling: hosted Codex bypassed bounded tail reads; Kimi/Vibe read full histories; pre-replay publishing and prompt snapping caused viewport jumps.
- [x] Fix loading/rendering so the latest conversation view appears promptly and stays anchored while history arrives.
- [x] Simplify the default status strip while retaining custom layouts; add passive usage refresh, freshness and reset countdown to the limit prompt.
- [x] Create an ASCII editor preview of a static, blocky colour fade confined to the last 30% of the session header.
- [x] Repair the Keys screen: readable bindings, clear editing actions and compact layout.
- [x] Validate regressions, record measurements and rebuild.

## Completed settings repair and local compaction

- [x] Inspect settings regressions and local compaction support: generic renderer mislabels actions; tev1 is a decision classifier.
- [x] Restore compact, visible list-and-detail settings; distinguish actions from choices and account information; add mouse selection and scrolling without background fall-through.
- [x] Check Ollama compaction eligibility: tev1 is a decision classifier, so show the limitation; support suitable local summarizers with an explicit saved default and unchanged-session guard.
- [x] Verify responsive layouts (58/80/140/220 columns), keyboard/mouse behavior, full repository tests, focused race checks and installed build.

## Completed performance and interaction pass

- [x] **Performance:** frame-local account/status caching and quota early exits remove about 40 KB of allocation per frame (13.4%); matched benchmarks and limitations recorded.
- [x] **Consistent row controls:** Space opens/closes transcript and task rows across every harness. Enter sends only from the composer; navigation cannot send a draft accidentally.
- [x] **Visible agent exchanges:** show sender → receiver, harnesses and message content on both sides of inter-agent sends; identify subagent handbacks.
- [x] **Background-task usability:** compact task summaries; output before command internals; elapsed/last-activity information; distinguish waiting, quiet, failed and completed without claiming silence proves a hang.
- [x] **Consistent agent labels:** every list row names its harness, including Claude Code and Kimi; long titles preserve the identity badge.
- [x] **Validation:** keyboard safety, exchange delivery/attribution, background diagnostics, performance benchmarks and regression tests.

## Completed previous pass

- [x] Full performance/allocation review and reproducible export benchmark.
- [x] Bounded handoff export, retained steering instructions and safe destination initialization.
- [x] Models/Harnesses settings, native account information, Vibe model defaults, Kimi/Vibe/Gemini integrations.
- [x] Ollama discovery, startup on demand and context controls.
- [x] Duplicate prompt fix, full-message/paste/image viewer and first/latest navigation.
- [x] Model/effort commands, session Shift+Tab controls and contextual F1 help.
- [x] Header colours and authoritative session identity.
- [x] Persistent multi-model discussion with bounded rounds, cancellation and replay protection.

Performance evidence and remaining findings: [performance review](performance-review-2026-10-01.md).

Verification: `TMPDIR=/tmp go test ./...` passed; focused host/conversation/CLI/UI race checks passed; local binary built and installed. Live authenticated provider conversations were not run.

Settings repair verification: full `TMPDIR=/tmp go test ./...` passed; focused UI checks passed after the final discovery/layout edits; focused compaction/host/Ollama race checks passed. Rebuilt and atomically installed `/Users/lw/go/bin/rush`; backup `/tmp/rush-before-settings-dizw3xz5`. No live compaction, generation or session restart was performed. External compaction requires an updated protocol-9 host.

Stable UI pass verification: full `TMPDIR=/tmp go test ./...` passed, focused loading/scrolling race checks passed, and Keys layout/capture/practice checks passed. Built and atomically installed `/Users/lw/go/bin/rush`; backup `/tmp/rush-before-stable-_b6yr0om`. Existing sessions were not restarted. Header ASCII preview is `docs/header-preview.txt`.

Community/permissions verification: full `TMPDIR=/tmp go test ./...` passed. Focused community storage/CLI/UI and permission UI/host/ACP race checks passed. Multi-process posting preserves replies; stale refreshes preserve drafts and selection; unchanged boards avoid repeat decoding. Permission rejection preserves the previous mode and applying the picker does not send drafts. Built and atomically installed `/Users/lw/go/bin/rush`; backup `/tmp/rush-before-community-9v2_m7pz`. Existing sessions were not restarted and no live provider permissions were changed. The shared adapter level fixture was aligned with the concurrent Kimi task’s documented promotion to tested.

Compact status configuration: replaced the requested duplicated top segments with one compact line, preserving the Agent header layout. Original saved at `/tmp/rush-bars-before-compact-ladybty0`. Other users’ custom layouts remain unchanged.

Compact status/idle lifecycle verification: full `TMPDIR=/tmp go test ./...` passed; focused host and UI sleep/discussion race checks passed. Real-process retirement, concurrent wake, saved metadata, held queues, view-only cold reopen, draft restoration and discussion resume are covered. Fixed a nil settings-store guard in the concurrent minimap integration. Built and atomically installed `/Users/lw/go/bin/rush`; backup `/tmp/rush-before-idle-b9mx02bf`. Existing live sessions were not interrupted. Protocol-10 hosts support clean idle exit; explicit messages resume the saved conversation.

Interaction/location verification: full `TMPDIR=/tmp go test ./...` passed after concurrent pane edits; focused UI/conversation/fleet race checks passed. Final UI suite passed after caching header hit targets to avoid account rendering during hover. Header edge geometry, queue hit mapping, draft-safe tab clicks, unchanged transcript content on hover, shell attribution/progress and location consistency are covered. Built and atomically installed `/Users/lw/go/bin/rush`; backup `/tmp/rush-before-interaction-_k84mbnm`. Shell process tracking requires observable local metadata; remote/opaque processes remain unknown. No separate customized Claude Code source was identified, so this extends Rush’s existing diagnostics rather than claiming a source transplant.

Header colour restoration: restored the disconnected headerWash call, moved the pattern boundary to ceil(width * 0.60), and saved three sketches in docs/header-preview.txt. Focused colour/style/contrast/boundary and pane geometry/tab regressions pass. Built and atomically installed; backup `/tmp/rush-before-header-colour-1oh8z1lp`.

Header top-padding correction: moved the chrome blank row above the title; tabs now touch the border. Header height/body viewport unchanged. Tab and harness-label targets follow shared row constants. Focused header/geometry/pointer/colour tests passed and build installed; backup `/tmp/rush-before-top-gap-eyqmoxy8`. Full UI run failed only TestAccountsGroupedByAgent because concurrent provider styling removed its expected glyph/profile star; left unrelated provider edits intact.

Chunkier header colour: patches now span four columns by two rows across all four header rows, with a broader coloured middle and unchanged 60–100% bounds. Plain-left tab surfaces retained. Focused colour/contrast/text/width/header/pointer checks passed; built and installed. Backup `/tmp/rush-before-chunky-header-pynysr5x`.

Page top spacing: moved wide-layout content and body up one row while preserving every icon row; icon foot shares the page-navigation row. Brighter cap/ridge colours, updated navigation targets and width-aware settings tabs. Focused icon, layout, header, pointer and settings-tab checks passed. Built and installed; backup `/tmp/rush-before-page-spacing-tww7vi2f`.

## Compatibility matrix — 2026-10-01

- [x] Extract every registered harness/provider pairing and capability from the current adapters.
- [x] Check capability caveats, Codex compaction/rewind and the shared Kimi conversation UI.
- [x] Publish a complete matrix and verify it against the adapter registry. Results: [matrix](agent-compatibility-matrix.md), [full CSV](agent-compatibility-matrix.csv). Registry/interface/level tests passed; inherited cross-provider rewind declarations are flagged, not counted as working.

## Compatibility fixes

- [x] Verify and complete Codex native compaction, including idle/error/event behavior.
- [x] Correct unsupported inherited rewind claims and simplify shared agent-exchange attribution.
- [x] Run focused regressions, refresh the compatibility matrix, and build/install the verified change. Full repository tests and focused race checks passed; installed with the CPU fixes.

## Live CPU spikes

- [x] Identify the hot Rush process and capture live CPU profiles during spikes: UI PID 73731 at 83.9%; live sample and 15-second CPU/allocation profile captured.
- [x] Fix measured hotspots and the confirmed startup discovery stall: pace and serialize automatic disk scans, cache unchanged stopped rows, and use a local Antigravity catalog during host startup.
- [x] Verify performance and regressions, then install the corrected build. Controlled disk-scan CPU: 92.99% → 10.69%, identical counts. Full suite and focused race checks passed. Atomic install completed; backup `/tmp/rush-before-cpu-nal23hbv`. Existing UI needs `#reload`; post-reload live CPU remains unmeasured.
