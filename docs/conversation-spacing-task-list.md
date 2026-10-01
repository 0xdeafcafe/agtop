# Conversation spacing and loading

- [x] Trace the conversation top inset and activity placement during loading.
- [x] Remove the unwanted inset and keep activity panels anchored while history loads.
- [x] Verify rendering and loading regressions, build and install.

Removed the trailing blank row from the pane header and the leading blank loading/empty placeholder row. While host replay is incomplete, auxiliary panels remain in the bottom dock rather than joining the placeholder as transcript content. Once ready, panels continue to scroll with chat as before.

Validation: UI suite passed; focused loading/replay/panel race checks passed; regression covers 60×30 and 120×50 panes, stable subagent/background/queue/composer placement during loading, the ready transition, queue hit targets and later history scrolling. Built and atomically installed `/Users/lw/go/bin/rush`; source and installed hashes match. Existing sessions were not restarted. Use `#reload`.
