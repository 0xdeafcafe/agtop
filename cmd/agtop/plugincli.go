package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/0xdeafcafe/agtop/internal/plugin"
)

// pluginCLI runs `agtop <plugin> <command> [args]`, one of the commands a
// plugin adds, through the broker; it returns the exit code.
func pluginCLI(name string, m plugin.Manifest, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(stdout, pluginCLIUsage(name, m))
		return 0
	}
	if _, ok := m.CLICommand(args[0]); !ok {
		fmt.Fprintf(stderr, "agtop: %s has no command %q\n\n%s", name, args[0], pluginCLIUsage(name, m))
		return 2
	}
	cwd, _ := os.Getwd()
	ctx, cancel := context.WithTimeout(context.Background(), plugin.CLITimeout+5*time.Second)
	defer cancel()
	res, err := plugin.RunCLI(ctx, name, plugin.CLIRun{Command: args[0], Args: args[1:], Cwd: cwd})
	if err != nil {
		fmt.Fprintln(stderr, "agtop:", err)
		return 1
	}
	fmt.Fprint(stdout, res.Stdout)
	fmt.Fprint(stderr, res.Stderr)
	return res.Exit
}

func pluginCLIUsage(name string, m plugin.Manifest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "agtop %s: from the plugin %s\n\n", name, name)
	for _, c := range m.CLI {
		fmt.Fprintf(&b, "  agtop %s %s", name, c.Name)
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
	b.WriteString("\nFrom plugins (\"agtop <plugin> help\" for their commands):\n\n")
	for _, n := range names {
		cs := make([]string, 0, len(ps[n].CLI))
		for _, c := range ps[n].CLI {
			cs = append(cs, c.Name)
		}
		fmt.Fprintf(&b, "  agtop %-11s %s\n", n, strings.Join(cs, ", "))
	}
	return b.String()
}
