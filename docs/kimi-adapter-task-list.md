# Kimi adapter

- [x] Check the current adapter against the installed Kimi CLI and identify gaps.
- [x] Finish Kimi-specific integration and add focused regression coverage.
- [x] Verify the adapter, run a live protocol smoke test, and build Rush.

Implemented Kimi Code 2 workspace/session discovery and wire replay with bounded tail reads, timestamp cutoffs, text/thinking, tool calls/results, turn token usage, interruptions and compaction. Retained legacy context history. Kimi's update action uses its own `upgrade` command, rather than advertising the older Python package. Marked support tested after successful real CLI checks.

Primary references: https://www.kimi.com/code/docs/en/kimi-code-cli/reference/kimi-acp and https://www.kimi.com/code/docs/en/kimi-code-cli/configuration/data-locations.html. Confirmed actual installed 2.0.1 event schemas locally without exporting private conversation content.

Verification: ACP, adapter registration, updater, host and UI test suites passed. Real Kimi Code 2.0.1 tests passed for handshake, adapter start, configured model selection, plan/default mode switching, one no-tool model prompt returning the expected marker, saved-history discovery/replay, and resume of the same session. `go build -o /Users/lw/go/bin/rush ./cmd/rush` succeeded. Use `#reload` to load this build; `#with kimi` selects Kimi for new sessions.
