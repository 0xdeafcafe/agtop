package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/x/term"

	"github.com/0xdeafcafe/agtop/internal/plugin"
	"github.com/0xdeafcafe/agtop/internal/plugind"
)

const pluginUsage = `agtop plugin — sandboxed plugins

  agtop plugin list            what's installed, approved and running
  agtop plugin check <name>    check a plugin and show what approving it allows
  agtop plugin approve <name>  read what a plugin may do, and let it run
  agtop plugin revoke <name>   stop it running
  agtop plugin logs <name>     where its output goes

Plugins live in %s/<name>, each with a plugin.json.
`

func pluginCmd(args []string) error {
	if len(args) == 0 {
		fmt.Printf(pluginUsage, plugin.Root())
		return nil
	}
	switch args[0] {
	case "list", "ls":
		return pluginList()
	case "check":
		if len(args) != 2 {
			return fmt.Errorf("usage: agtop plugin check <name>")
		}
		p, err := plugin.Load(filepath.Join(plugin.Root(), args[1]))
		if err != nil {
			return err
		}
		fmt.Print(describe(p))
		if err := plugin.Supported(); err != nil {
			return err
		}
		if _, err := (plugin.Launch{Plugin: p}).Program(); err != nil {
			return fmt.Errorf("its program: %w", err)
		}
		a, ok := plugin.Approvals()[p.Name]
		switch d, _ := plugin.Digest(p.Dir); {
		case !ok:
			fmt.Printf("\nValid, not approved. To run it: agtop plugin approve %s\n", p.Name)
		case d != a.Digest:
			fmt.Printf("\nValid, changed since approval. To run it: agtop plugin approve %s\n", p.Name)
		default:
			fmt.Println("\nValid and approved.")
		}
		return nil
	case "approve":
		if len(args) != 2 {
			return fmt.Errorf("usage: agtop plugin approve <name>")
		}
		return pluginApprove(args[1])
	case "revoke":
		if len(args) != 2 {
			return fmt.Errorf("usage: agtop plugin revoke <name>")
		}
		if err := plugin.Revoke(args[1]); err != nil {
			return err
		}
		fmt.Printf("%s revoked; it stops now and won't start again.\n", args[1])
		return plugind.Reload()
	case "logs":
		if len(args) != 2 {
			return fmt.Errorf("usage: agtop plugin logs <name>")
		}
		fmt.Println(plugin.LogPath(args[1]))
		fmt.Println(plugin.BrokerLog())
		return nil
	}
	fmt.Printf(pluginUsage, plugin.Root())
	return fmt.Errorf("unknown command %q", args[0])
}

func pluginList() error {
	installed, bad := plugin.Installed()
	approvals := plugin.Approvals()
	running := map[string]plugind.Status{}
	if st, err := plugind.Ask(); err == nil {
		for _, s := range st {
			running[s.Name] = s
		}
	}
	if len(installed) == 0 && len(bad) == 0 {
		fmt.Printf("No plugins. Put one in %s/<name>.\n", plugin.Root())
	}
	if err := plugin.Supported(); err != nil {
		fmt.Println(err)
	}
	for _, p := range installed {
		status := "not approved"
		if a, ok := approvals[p.Name]; ok {
			status = "approved"
			if d, err := plugin.Digest(p.Dir); err != nil || d != a.Digest {
				status = "changed since approval: approve again to run it"
			} else if s, ok := running[p.Name]; ok {
				status = s.State
				if s.PID > 0 {
					status += fmt.Sprintf(" (pid %d)", s.PID)
				}
				if s.Restarts > 0 {
					status += fmt.Sprintf(", %d restarts", s.Restarts)
				}
				if s.Error != "" && s.State != "running" {
					status += ": " + s.Error
				}
			}
		}
		fmt.Printf("%-16s %s\n", p.Name, status)
		if p.Description != "" {
			fmt.Printf("%-16s %s\n", "", p.Description)
		}
	}
	names := make([]string, 0, len(bad))
	for n := range bad {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Printf("%-16s broken: %v\n", n, bad[n])
	}
	return nil
}

func pluginApprove(name string) error {
	if err := plugin.Supported(); err != nil {
		return err
	}
	p, err := plugin.Load(filepath.Join(plugin.Root(), name))
	if err != nil {
		return err
	}
	fmt.Print(describe(p))
	if !term.IsTerminal(os.Stdin.Fd()) {
		return fmt.Errorf("approving needs you at a terminal")
	}
	fmt.Print("\nLet it run? [y/N] ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
		fmt.Println("Not approved.")
		return nil
	}
	if err := plugin.Approve(p); err != nil {
		return err
	}
	fmt.Printf("Approved. If any file in %s changes, it stops until you approve it again.\n", p.Dir)
	fmt.Println("New sessions get its agents, prompt and tools; running ones get them when Claude Code next starts.")
	return plugind.Reload()
}

// describe says in plain words what approving p allows.
func describe(p plugin.Plugin) string {
	var b strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&b, f, a...) }
	w("%s", p.Name)
	if p.Version != "" {
		w(" %s", p.Version)
	}
	w("\n")
	if p.Description != "" {
		w("  %s\n", p.Description)
	}
	w("\nIt runs %s", strings.Join(p.Command, " "))
	if p.Proto() == plugin.ProtoMCP {
		w(" as an MCP server")
	}
	w(", sandboxed:\n")
	w("  • reads its folder, %s, and the system's libraries\n", p.Dir)
	for _, r := range p.Read {
		w("  • also reads %s\n", r)
	}
	if len(p.Write) == 0 {
		w("  • writes only %s\n", plugin.DataDir(p.Name))
	} else {
		w("  • writes %s, and %s\n", plugin.DataDir(p.Name), strings.Join(p.Write, ", "))
	}
	if len(p.Network) == 0 {
		w("  • no network\n")
	} else {
		w("  • reaches only %s\n", strings.Join(p.Network, ", "))
	}
	w("  • starts no other programs itself, at most %d MB of memory\n", p.Memory()>>20)
	if len(p.Exec) > 0 {
		names := make([]string, 0, len(p.Exec))
		for n := range p.Exec {
			names = append(names, n)
		}
		sort.Strings(names)
		w("\nOutside the sandbox, as you, agtop runs for it (with any arguments it adds):\n")
		for _, n := range names {
			w("  • %s\n", strings.Join(p.Exec[n], " "))
		}
	}

	var grants []string
	if p.HasTools() {
		grants = append(grants, "offers its tools to every agtop-mode session (each call asks you first, as any tool does)")
	}
	if len(p.Agents) > 0 {
		names := make([]string, 0, len(p.Agents))
		for n := range p.Agents {
			names = append(names, p.Name+":"+n)
		}
		sort.Strings(names)
		grants = append(grants, "adds subagents to every agtop-mode session: "+strings.Join(names, ", "))
	}
	if p.Prompt != "" {
		grants = append(grants, fmt.Sprintf("adds %d characters to every agtop-mode session's system prompt:\n      %s",
			len(p.Prompt), strings.ReplaceAll(clip(p.Prompt, 400), "\n", "\n      ")))
	}
	if p.Can(plugin.CapList) {
		grants = append(grants, "sees every agtop-mode session's name, folder, branch, state, cost and context use (not what was said)")
	}
	if p.Can(plugin.CapStart) {
		grants = append(grants, fmt.Sprintf("starts sessions on your account in %s, never in a mode that skips asking you;\n"+
			"      tools your Claude Code settings already allow run there without asking", strings.Join(p.Workspaces, ", ")))
	}
	if p.Can(plugin.CapRead) {
		grants = append(grants, "follows what the sessions it started say")
	}
	if p.Can(plugin.CapSend) {
		grants = append(grants, "sends messages to the sessions it started")
	}
	if p.Can(plugin.CapQueue) {
		grants = append(grants, fmt.Sprintf("queues messages, marked as its own, to any session in %s that asks you before acting", strings.Join(p.Workspaces, ", ")))
	}
	if p.Can(plugin.CapControl) {
		grants = append(grants, "interrupts and stops the sessions it started")
	}
	if len(grants) > 0 {
		w("\nIt:\n")
		for _, g := range grants {
			w("  • %s\n", g)
		}
	}
	return b.String()
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
