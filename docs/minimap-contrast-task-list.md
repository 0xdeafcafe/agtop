# Quieter minimap

- [x] Soften dots and remove the full-height contrasting background while retaining the viewport marker.
- [x] Check rendering and rebuild Rush.

Use the existing theme palette at 36% intensity against the terminal ground. Keep the viewport as the only raised surface, with a muted marker. Preserve map geometry and cached rendering.

Existing minimap tests passed; reviewed the rendered frame. Built and installed `/Users/lw/go/bin/rush`.
