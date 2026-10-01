# User message colour

- [x] Locate the user-message colours and select a quieter treatment.
- [x] Apply the neutral surface and muted accent consistently to user messages.
- [x] Check rendering, run relevant checks, and rebuild Rush.

Design: retain the terminal theme, monospace typography, left alignment and existing message layout. Use the theme's neutral surface for message backgrounds and its blue accent for the narrow author rail and “you” label. Keep the body text at its existing readable contrast. Large red/orange surfaces imply failure and should not identify ordinary user input.

Verified the rendered queued-message example; conversation tests passed. Rebuilt `/Users/lw/go/bin/rush`.
