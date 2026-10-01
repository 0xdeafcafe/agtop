# Projects storage and conversation space

- [x] Identify the temp growth and how Projects discovers, measures and cleans session storage.
- [x] Fix storage reporting/cleanup and reduce the reported folder where verified safe.
- [x] Make auxiliary conversation panels scroll away and simplify their colours.
- [x] Verify storage and viewport behavior, then rebuild Rush.

## Findings and layout

Reported folder: 37 GiB (about 40 GB), 1,290 top-level entries. Verified 843 generated Vite ssr/client caches, 1,253,473 hash-named files, from stopped session f44d8bb1. No open cache files found; a shell has only the tmp root as its working directory. Clean only verified day-old cache files, preserving every other artifact.

Projects must include active and old session storage, report sizes progressively, and identify their paths. Keep cleanup blocked while a session runs.

Conversation design uses the existing monospace hierarchy and a neutral charcoal surface (#1e1c1a) across queue, background and question panels. Text stays warm white (#e2ddd3), secondary grey (#a8a298); orange (#d97757) marks focus and amber (#e5b567) marks a pending decision. Remove competing purple/blue/amber full-panel fills. Panels join the scrollable history; composer and activity remain fixed. Mouse hit targets follow the visible rows.

## Completed

- Removed 34,861,068,288 bytes of verified Vite caches and 4,613,107,712 bytes of old Go, TSX and Node compiler caches. Total reclaimed: 39,474,176,000 bytes (39.47 GB). Folder now measures 671,834,112 bytes (640.7 MiB), versus 40,146,010,112 bytes before. Preserved Claude files, history, attachments and other artifacts. Updated this session's cached size.
- Projects includes running sessions and old stopped sessions, shows their paths, publishes measurements one session at a time, and completes each scan queue before starting another. Hosted mode measures storage when Projects is visible. Stopped-session sizes eventually refresh after external changes.
- Auxiliary panels are part of the scrollable conversation. Current activity stays visible, follows short output above panels at the tail, and pins above the composer when reading history. Queue/subagent/job selection and clipped controls follow the rendered viewport; keyboard question navigation restores the panel. All panel surfaces now use neutral chrome.
- Full UI, fleet, conversation and state suites passed. Additional focused tests passed after the final storage and modal refinements. Reviewed generated terminal frames at the tail and scrolled into history. `git diff --check` passed.
- Built and installed `/Users/lw/go/bin/rush`; `#reload` loads it in the current UI.
