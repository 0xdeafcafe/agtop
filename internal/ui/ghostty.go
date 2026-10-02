package ui

import (
	"embed"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// #ghostty: LangWatch's light and dark themes for Ghostty, following the
// system's appearance. The themes go in Ghostty's themes folder, and its
// config gets one theme line; colours the config set itself are commented
// out (they'd beat the theme) and come back with #ghostty off.

//go:embed ghosttythemes
var ghosttyThemes embed.FS

// ghosttyTheme is the line rush puts in Ghostty's config (which takes no
// comment after a value), known again by being exactly this.
const ghosttyTheme = "theme = light:langwatch-light,dark:langwatch-dark"

// ghosttyHeld marks a config line rush commented out, to put back.
const ghosttyHeld = "# rush #ghostty held: "

// ghosttyDir is Ghostty's XDG folder, where it looks for themes.
func ghosttyDir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "ghostty")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "ghostty")
}

// ghosttyConfig is the config file Ghostty reads that rush edits: the first
// that exists, the XDG one if none does.
// ponytail: one file; a theme or colours set in another config Ghostty also
// loads still win. Edit every one found if that bites.
func ghosttyConfig() string {
	home, _ := os.UserHomeDir()
	app := filepath.Join(home, "Library", "Application Support", "com.mitchellh.ghostty")
	for _, p := range []string{
		filepath.Join(app, "config.ghostty"), filepath.Join(app, "config"),
		filepath.Join(ghosttyDir(), "config.ghostty"), filepath.Join(ghosttyDir(), "config"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return filepath.Join(ghosttyDir(), "config.ghostty")
}

// ghosttyColourKey is whether a config line sets a colour the theme sets.
func ghosttyColourKey(line string) bool {
	k, _, ok := strings.Cut(line, "=")
	if !ok {
		return false
	}
	switch strings.TrimSpace(k) {
	case "theme", "background", "foreground", "palette", "cursor-color", "cursor-text",
		"selection-background", "selection-foreground":
		return true
	}
	return false
}

// ghosttyOn adds rush's themes to config's text: colours it set held as
// comments, and the theme line at the end.
func ghosttyOn(config string) string {
	var out []string
	for _, l := range strings.Split(strings.TrimRight(config, "\n"), "\n") {
		switch {
		case l == ghosttyTheme:
			continue
		case ghosttyColourKey(l):
			l = ghosttyHeld + l
		}
		out = append(out, l)
	}
	if len(out) == 1 && out[0] == "" {
		out = nil
	}
	return strings.Join(append(out, ghosttyTheme), "\n") + "\n"
}

// ghosttyOff is config's text with rush's line out and what it held back.
func ghosttyOff(config string) string {
	var out []string
	for _, l := range strings.Split(config, "\n") {
		if l == ghosttyTheme {
			continue
		}
		out = append(out, strings.TrimPrefix(l, ghosttyHeld))
	}
	return strings.Join(out, "\n")
}

func ghosttyInstall() error {
	dir := filepath.Join(ghosttyDir(), "themes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"langwatch-dark", "langwatch-light"} {
		b, _ := ghosttyThemes.ReadFile("ghosttythemes/" + name)
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			return err
		}
	}
	path := ghosttyConfig()
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(ghosttyOn(string(b))), 0o644)
}

func ghosttyRemove() error {
	path := ghosttyConfig()
	if b, err := os.ReadFile(path); err == nil {
		if err := os.WriteFile(path, []byte(ghosttyOff(string(b))), 0o644); err != nil {
			return err
		}
	}
	for _, name := range []string{"langwatch-dark", "langwatch-light"} {
		if err := os.Remove(filepath.Join(ghosttyDir(), "themes", name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func ghosttyIsOn() bool {
	b, err := os.ReadFile(ghosttyConfig())
	return err == nil && strings.Contains(string(b), ghosttyTheme)
}

// ghosttyCommand is #ghostty.
func (m *Model) ghosttyCommand(arg string) tea.Cmd {
	switch strings.TrimSpace(arg) {
	case "on":
		return func() tea.Msg {
			if err := ghosttyInstall(); err != nil {
				return doneMsg{err: err}
			}
			return doneMsg{text: "LangWatch themes on in Ghostty, light and dark following the system · ⌘⇧, in Ghostty loads them"}
		}
	case "off":
		return func() tea.Msg {
			if err := ghosttyRemove(); err != nil {
				return doneMsg{err: err}
			}
			return doneMsg{text: "LangWatch themes off · Ghostty's own colours are back · ⌘⇧, in Ghostty loads them"}
		}
	}
	return later(ghosttyIsOn, func(m *Model, on bool) tea.Cmd {
		if on {
			m.flash("Ghostty is on LangWatch's themes · #ghostty off", false)
		} else {
			m.flash("#ghostty on puts Ghostty on LangWatch's light and dark themes, following the system", false)
		}
		return nil
	})
}
