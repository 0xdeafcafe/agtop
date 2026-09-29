package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"

	"github.com/0xdeafcafe/rush/internal/plugin"
)

// pluginCLI runs `rush <plugin> <command> [args]`, one of the commands a
// plugin adds, through the broker; it returns the exit code.
func pluginCLI(name string, m plugin.Manifest, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(stdout, pluginCLIUsage(name, m))
		return 0
	}
	if _, ok := m.CLICommand(args[0]); !ok {
		fmt.Fprintf(stderr, "rush: %s has no command %q\n\n%s", name, args[0], pluginCLIUsage(name, m))
		return 2
	}
	cwd, _ := os.Getwd()
	ctx, cancel := context.WithTimeout(context.Background(), plugin.CLITimeout+5*time.Second)
	defer cancel()
	res, err := plugin.RunCLI(ctx, name, plugin.CLIRun{Command: args[0], Args: args[1:], Cwd: cwd})
	if err != nil {
		fmt.Fprintln(stderr, "rush:", err)
		return 1
	}
	fmt.Fprint(stdout, forTerminal(stdout, res.Stdout))
	fmt.Fprint(stderr, forTerminal(stderr, res.Stderr))
	return res.Exit
}

// forTerminal is a plugin's output as it may reach a terminal: no control
// characters but newlines and tabs, so it can't set the clipboard, the
// title or the screen with escape sequences. Piped, it goes as it came.
func forTerminal(w io.Writer, s string) string {
	if f, ok := w.(*os.File); !ok || !term.IsTerminal(f.Fd()) {
		return s
	}
	return stripControl(s)
}

func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r >= 0x20 && r != 0x7f && (r < 0x80 || r > 0x9f) {
			return r
		}
		return -1
	}, s)
}

func pluginCLIUsage(name string, m plugin.Manifest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "rush %s: from the plugin %s\n\n", name, name)
	for _, c := range m.CLI {
		fmt.Fprintf(&b, "  rush %s %s", name, c.Name)
		if c.Usage != "" {
			b.WriteString(" " + c.Usage)
		}
		fmt.Fprintf(&b, "\n        %s\n", c.Description)
	}
	return b.String()
}

// pluginsUsage is the help's lines for the commands plugins add.
func pluginsUsage() string {
	ps := plugin.CLIPlugins()
	if len(ps) == 0 {
		return ""
	}
	names := make([]string, 0, len(ps))
	for n := range ps {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("\nFrom plugins (\"rush <plugin> help\" for their commands):\n\n")
	for _, n := range names {
		cs := make([]string, 0, len(ps[n].CLI))
		for _, c := range ps[n].CLI {
			cs = append(cs, c.Name)
		}
		fmt.Fprintf(&b, "  rush %-11s %s\n", n, strings.Join(cs, ", "))
	}
	return b.String()
}
