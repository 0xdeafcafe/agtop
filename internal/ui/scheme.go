package ui

import (
	"os"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// The terminal saying the system went dark or light (mode 2031, which
// Ghostty, kitty and others report): rush's colours are asked for again
// then, rather than at the next focus.
type (
	darkScheme  = uv.DarkColorSchemeEvent
	lightScheme = uv.LightColorSchemeEvent
)

// startColours asks for the terminal's colours and to be told when the
// system's light or dark changes. Asked once: a terminal that answers the
// mode with a report would otherwise ask again for ever.
var startColours = tea.Batch(askColours, tea.Raw(ansi.SetModeLightDark))

// StopSchemeReports turns mode 2031 off again as rush exits, so the
// reports don't land in the shell.
func StopSchemeReports() { _, _ = os.Stdout.WriteString(ansi.ResetModeLightDark) }
