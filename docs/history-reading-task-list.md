# Readable conversation history

- [x] Inspect turn rendering, folding state, navigation and the supplied screenshot.
- [x] Redesign history to retain useful text, with explicit bulk controls and the latest turn open.
- [x] Verify rendering, navigation and folding behavior; rebuild Rush.

Design: retain the terminal font and theme-adapted palette: text #e2ddd3, secondary #a8a298, metadata #8a847a, surface #1a1816, selection #2c2824. Bold prompts, regular answers, secondary metadata. Left align prose; separate metadata from the text so neither competes for horizontal space. Short replies remain whole; long replies preview several wrapped lines with an explicit continuation. Routine wakeups without a reply stay compact. Full turns keep their existing tool folds. Individual fold overrides remain stable when new turns arrive.

    ▸ ✓ #12 you                             17 steps  59s
        Build out the simulation cases.

        Added the missing simulation cases and verified…
        … open turn for full answer

The screenshot's primary problem is competing, truncated prompt and answer text. Stacking those words addresses it while retaining the existing visual language. Bulk controls change turn visibility only, with the latest/live turn open and explicit per-turn choices honored thereafter.

Controls: `#expand` / Ctrl+] then E opens every turn. `#collapse` / Ctrl+] then S previews older turns while keeping latest/live/error/waiting turns open. Space changes one turn. These commands preserve tool folds and drafts, and apply to history loaded later.

Validation: `go test ./internal/convo ./internal/ui ./internal/keymap` passed. Preview checks cover 20/40/80/180-column panes, Unicode wrapping, long replies, full expansion, manual overrides and cache invalidation. UI checks cover both commands and key chords, retained drafts and tool choices, and delayed whole-history loading. Reviewed actual renderer output in `/tmp/rush-history-preview.txt`. Installed build succeeded at `/Users/lw/go/bin/rush`.

Performance on this Apple M1 Max: synthetic 300-turn cached rendering ~0.244 ms/frame and streaming ~0.568 ms/frame; invalidating and redrawing the preview of a 10,000-line reply ~0.016 ms. These are local timings, not before/after speedup claims.
