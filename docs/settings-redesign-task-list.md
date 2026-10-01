# Settings redesign

- [x] Inspect every settings page and settle the shared layout and navigation.
- [x] Implement the complete settings shell, page hierarchy, forms and responsive interaction.
- [x] Verify all settings pages, keyboard/mouse behavior and regression tests; build the result.

Design: terminal-native, left-aligned workspace. Reuse theme-aware ink (#e2ddd3), secondary ink (#a8a298), surface (#1e1c1a), focus (#d97757), and success (#7fbf8a); preserve light and accessible themes. Bold only page/section names; plain terminal type throughout. A quiet section rail leads to aligned settings, with a stable help/preview panel. At narrow widths, help sits below the scrolling controls. No per-row descriptive paragraphs; explain the selected setting once. Models, harnesses, accounts, general, appearance, keys, plugins and updates share the heading and spacing. Existing task-oriented browser controls stay intact.

Wide form: sections | controls | help and preview.
Compact form: controls, then selected-setting help, then keyboard actions.

Delivered: all eight settings pages share the new navigation and heading. Accounts and Appearance are directly accessible; account/profile entry points open Accounts. General, Appearance, Plugins and Updates use bounded forms with persistent explanations. Models, Harnesses and Accounts share catalog geometry, scrolling and mouse selection. Narrow navigation retains the selected page; Keys preserves editing controls at 44×20. Home/End and Ctrl+Up/Down navigate forms, and `?` expands complete explanations. Appearance keeps its panel geometry when moving between settings with and without previews.

Validation: full `TMPDIR=/tmp go test ./...` passed; focused UI race checks passed; the final stationary-preview refinement passed responsive rendering, pointer, help and preview tests. Layout coverage: 44×20, 58×24, 80×30, 140×40 and 200×50; both first and last controls on every page. `git diff --check` passed. Rendered text previews are in `/tmp/rush-settings-final`.

Built and atomically installed `/Users/lw/go/bin/rush`; installed SHA-256 matches the built binary. Backup: `/tmp/rush-before-settings-redesign-tclgswtz`. Existing sessions were not restarted. Use `#reload` to load the new UI.
