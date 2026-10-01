# Conversation minimap

- [x] Inspect conversation rendering, header styling, scrolling, caching and settings.
- [x] Implement a cached colourful minimap with viewport tracking and mouse navigation; simplify the flagged header.
- [x] Add its setting, verify navigation and performance, and rebuild Rush.

## Design

Keep terminal monospace: existing bold headings, regular body, quiet metadata. Palette follows Rush and its light/colour-blind themes: warm charcoal #1e1c1a ground, sky #8fb3d9 prompts, terracotta #d97757 tools, sage #7fbf8a success, coral #e0685c failures, lavender #b2a0d6 accent. Plain header surface removes the distracting coloured patch.

```
conversation text                         │ miniature text
                                          ▌ shaded viewport
current activity                          │ miniature text
---------------------------------------------------------
background work / composer
```

A 12-column right rail (one gap, viewport track, ten braille columns) maps actual rendered rows, including folds and wraps. It stays out of the composer. Shaded viewport plus a solid marker conveys position without relying on colour alone. Clicking jumps; dragging preserves the grabbed offset. Narrow panes hide the rail to preserve reading space. Settings > General > Look turns it off. Cache miniature rows per host; only changed rows are decoded, and scrolling reuses the complete raster. Theme/width/content changes invalidate the appropriate cache. No animation timer or additional transcript parsing.

## Verification

- Full UI suite passed, including minimap navigation, cache invalidation, settings, Unicode, activity placement and relayout scheduling. Conversation, state and keymap suites passed.
- Visual review used rendered terminal frames for eight expanded exchanges and a 300-turn history. Density averaging preserves whitespace when the history is compressed; the viewport uses a shaded band and solid marker.
- M1 Max microbenchmarks, 30,000 rendered rows: cold projection about 15 ms, warm content check about 0.16 ms with zero allocations, synthetic streaming about 0.21 ms. Cached marker-only scrolling about 0.14 microseconds with zero allocations. These measure the minimap, not the entire terminal frame.
- Fixed the layout scheduler and background warming to use the reduced conversation width, preventing idle redraw loops. Overlay controls retain mouse priority. Disabling the minimap releases its cache.
- Built and installed `/Users/lw/go/bin/rush`. Use `#reload` to load it in an existing UI.
