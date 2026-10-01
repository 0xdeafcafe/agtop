# History investigation

- [x] Locate saved history and trace view loading: earlier messages persist; host restarted between turns.
- [x] Reproduced zero history with a missing fleet profile; restore the directory from saved host configuration.
- [x] Verified regression, history/replay checks, and both earlier turns from the actual saved conversation; rebuilt /Users/lw/go/bin/rush.

Cause: live non-Claude rows may omit the profile directory. After a host restart, the pre-replay loader silently returned empty history. It now falls back to the saved host account directory while preserving an existing profile. No transcript data was lost. Use #reload in the running view to load the rebuilt binary.
