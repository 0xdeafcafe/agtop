# Conversation activity placement

- [x] Inspect the conversation viewport and dock boundary.
- [x] Keep activity in the conversation, pinning it at the viewport bottom while scrolled up.
- [x] Verify placement and scroll behavior, then rebuild Rush.

Corrected per the screenshot: activity is part of the conversation viewport, above the entire task/queue/composer dock. It follows output naturally for short conversations. When scrolled up, it occupies the bottom of the conversation viewport, below the more-output indicator. It is kept out of scroll storage to avoid duplicate or stale status and cannot intercept text selection.

Verification: activity placement, queue ordering, short-conversation layout, scroll anchoring, following the tail, long-prompt scrolling, queue and row-key tests passed. Installed `/Users/lw/go/bin/rush` build succeeded. Use `#reload` for this placement.
