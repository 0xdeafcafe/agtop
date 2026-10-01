# Subagents scrolling

- [x] Reproduce how the subagent list, preview and outer pane share scrolling and selection.
- [x] Fix scroll ownership and navigation so the screen has no competing nested viewport.
- [x] Verify long lists, long transcripts, mouse and keyboard behavior, then rebuild Rush.

Verified: 43-run keyboard navigation, independent mouse scrolling, stable preview during new output, preview click-to-open, and narrow layout. UI and conversation suites passed; terminal render reviewed. Rebuilt `/Users/lw/go/bin/rush`.
