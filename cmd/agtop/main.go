package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/daemon"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/host"
	"github.com/0xdeafcafe/agtop/internal/menubar"
	"github.com/0xdeafcafe/agtop/internal/plugind"
	"github.com/0xdeafcafe/agtop/internal/state"
	"github.com/0xdeafcafe/agtop/internal/statusline"
	"github.com/0xdeafcafe/agtop/internal/ui"
	"github.com/0xdeafcafe/agtop/internal/update"
)

var version = "0.1.0"

// A go install'd agtop knows which one it is.
func init() {
	if i, ok := update.Current(); ok {
		version = i.Short()
	}
}

const usage = `agtop — a lighter agents view for Claude Code

  agtop             open the view
  agtop on          make "claude agents" open this view (adds one line to your shell rc)
  agtop off         give "claude agents" back to Claude Code (instant, no shell reload)
  agtop status      show whether it is on
  agtop update      install the newest agtop, with go install
  agtop menubar     put agtop in the menu bar: usage, what's working, and
                    questions you can answer from their notification
  agtop menubar off take it out again
  agtop plugin      sandboxed plugins: list, approve, revoke
  agtop --dump      print what the view sees, for debugging
`

func main() {
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "--version", "version":
			fmt.Println("agtop", version)
			return
		case "--help", "-h", "help":
			fmt.Print(usage)
			return
		case "--dump":
			dump()
			return
		case "--render":
			render(args[1:])
			return
		case "--soak":
			profiled(func() { soak(args[1:]) })
			return
		case "attach":
			if len(args) < 2 {
				exitIf(fmt.Errorf("usage: agtop attach <id>"))
			}
			s := &daemon.Session{Client: daemon.Client{Account: state.Load().Config.ActiveAccount()}, Short: args[1]}
			exitIf(s.Run())
			return
		case "host":
			// agtop host run <id>: the detached process an agtop-mode session
			// lives in. agtop starts it; it is not meant to be run by hand.
			if len(args) < 3 || args[1] != "run" {
				exitIf(fmt.Errorf("usage: agtop host run <id>"))
			}
			var err error
			profiled(func() { err = host.Run(args[2]) })
			exitIf(err)
			return
		case "menubar":
			exitIf(menuBar(args[1:]))
			return
		case "plugin", "plugins":
			exitIf(pluginCmd(args[1:]))
			return
		case "plugind":
			// The plugin broker. agtop starts it when a plugin is approved;
			// it is not meant to be run by hand.
			exitIf(plugind.Run())
			return
		case "statusline":
			// Claude Code's statusLine command, set up by /statusline in a
			// Session: the session's JSON in, one line out.
			exitIf(statusline.Run(os.Stdin, os.Stdout))
			return
		case "update":
			exitIf(selfUpdate())
			return
		case "on":
			exitIf(turnOn())
			return
		case "off":
			exitIf(os.WriteFile(offFlag(), nil, 0o600))
			fmt.Println(`off — "claude agents" opens the native view. "agtop on" turns it back on.`)
			return
		case "status":
			if isOn() {
				fmt.Println(`on — "claude agents" opens agtop`)
			} else {
				fmt.Println(`off — "claude agents" opens the native view`)
			}
			return
		}
	}
	// 120 frames a second: a streamed delta reaches the terminal within
	// about 8ms of being drawn, and nothing is drawn when nothing changed.
	viewGC()
	p := tea.NewProgram(ui.New(state.Load(), version), tea.WithFPS(120))
	here := menubar.Here() // so the menu bar app comes back to this terminal
	var err error
	profiled(func() { _, err = p.Run() })
	here()
	if err != nil {
		fmt.Fprintln(os.Stderr, "agtop:", err)
		os.Exit(1)
	}
}

// viewGC tunes the collector for the view. Most of the time it holds
// 10-20 MB and allocates a few MB a second, so collecting at half again the
// live heap rather than double keeps some 8 MB less resident for about a
// collection a second more. A soft ceiling makes the collector give memory
// back after a big session is let go, without working harder the rest of
// the time.
func viewGC() {
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(50)
	}
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(128 << 20)
	}
}

func exitIf(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "agtop:", err)
		os.Exit(1)
	}
}

func offFlag() string { return filepath.Join(state.Dir(), "off") }

func isOn() bool {
	_, err := os.Stat(offFlag())
	if err == nil {
		return false
	}
	b, _ := os.ReadFile(rcFile())
	return strings.Contains(string(b), hookLine())
}

func hookPath() string { return filepath.Join(state.Dir(), "shell.sh") }
func hookLine() string { return `[ -f "` + hookPath() + `" ] && . "` + hookPath() + `"` }

func rcFile() string {
	home, _ := os.UserHomeDir()
	if strings.HasSuffix(os.Getenv("SHELL"), "bash") {
		return filepath.Join(home, ".bashrc")
	}
	return filepath.Join(home, ".zshrc")
}

// The hook only intercepts "claude agents"; every other claude call passes
// through untouched, and the off flag is read per call so toggling is instant.
const hook = `# added by "agtop on" — remove with "agtop off" or delete this file
claude() {
  if [ "$1" = "agents" ] && [ ! -f "%s" ] && command -v agtop >/dev/null 2>&1; then
    shift
    command agtop "$@"
  else
    command claude "$@"
  fi
}
`

func turnOn() error {
	if err := os.MkdirAll(state.Dir(), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(hookPath(), []byte(fmt.Sprintf(hook, offFlag())), 0o644); err != nil {
		return err
	}
	_ = os.Remove(offFlag())
	rc := rcFile()
	b, _ := os.ReadFile(rc)
	if !strings.Contains(string(b), hookLine()) {
		f, err := os.OpenFile(rc, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := f.WriteString("\n" + hookLine() + "\n"); err != nil {
			return err
		}
		fmt.Printf("on — added one line to %s. Open a new terminal (or: . %s)\n", rc, hookPath())
		return nil
	}
	fmt.Println(`on — "claude agents" opens agtop`)
	return nil
}

func dump() {
	st := state.Load()
	l := fleet.NewLoader(st)
	snap := l.Load(false)
	var targets []fleet.Target
	for _, a := range snap.Agents {
		if a.TranscriptPath != "" {
			targets = append(targets, fleet.Target{Key: a.Key, Path: a.TranscriptPath, Live: a.Live() || a.PID != 0})
		}
	}
	sc := fleet.NewScanner()
	t0 := time.Now()
	l.SetSpend(sc.Run(targets))
	sc.Flush()
	scan := time.Since(t0)
	l.Load(true)
	time.Sleep(time.Second)
	snap = l.Load(true)
	now := time.Now()
	for _, a := range snap.Agents {
		fmt.Printf("%-8s %-8s %-40.40s %6.1f%% %7.0fM %8.2f %9s %6s %d prs\n", a.ID, a.State, a.DisplayName,
			a.CPU, float64(a.Mem)/(1<<20), a.Spend.Cost, a.Elapsed(now).Round(time.Minute), a.Age(now).Round(time.Second), len(a.PRs))
	}
	for _, av := range snap.Accounts {
		fmt.Printf("account %s daemon=%v live=%d agents=%d spend=$%.2f today=$%.2f 5h=%.0f%% 7d=%.0f%%\n",
			av.Name, av.Daemon, av.Live, av.Agents, av.Spend, av.Today, av.Usage.FiveHour.Percent, av.Usage.SevenDay.Percent)
	}
	for _, r := range snap.Machine.Rows {
		fmt.Printf("proc %6d role=%d %-40.40s %5.1f%% %7.0fM n=%d\n", r.PID, r.Role, r.Label, r.CPU, float64(r.Mem)/(1<<20), r.Procs)
	}
	fmt.Printf("machine mem=%.0fM cpu=%.1f%% spares=%d scan=%s\n", float64(snap.Machine.TotalMem)/(1<<20), snap.Machine.TotalCPU, snap.Machine.Spares, scan)
}

// render prints one frame, e.g. agtop --render 160x45 tab
func render(args []string) {
	w, h := 160, 45
	if len(args) > 0 {
		fmt.Sscanf(args[0], "%dx%d", &w, &h)
	}
	var keys []tea.KeyPressMsg
	for _, k := range args[min(1, len(args)):] {
		switch k {
		case "tab":
			keys = append(keys, tea.KeyPressMsg{Code: tea.KeyTab})
		case "down":
			keys = append(keys, tea.KeyPressMsg{Code: tea.KeyDown})
		case "enter":
			keys = append(keys, tea.KeyPressMsg{Code: tea.KeyEnter})
		case "?":
			keys = append(keys, tea.KeyPressMsg{Code: '?', Text: "?"})
		default:
			if t, ok := strings.CutPrefix(k, "text="); ok {
				for _, r := range t {
					keys = append(keys, tea.KeyPressMsg{Code: r, Text: string(r)})
				}
				continue
			}
			if strings.HasPrefix(k, "ctrl+") {
				keys = append(keys, tea.KeyPressMsg{Code: rune(k[5]), Mod: tea.ModCtrl})
			}
		}
	}
	fmt.Println(ui.New(state.Load(), version).Frame(w, h, keys...))
}

// menuBar runs the menu bar icon, or its feed (which the icon runs).
func menuBar(args []string) error {
	st := state.Load()
	switch {
	case len(args) > 0 && args[0] == "feed":
		return menubar.Feed(os.Stdin, os.Stdout)
	case len(args) > 0 && args[0] == "off":
		st.Config.MenuBar, st.Config.MenuBarAsked = false, true
		menubar.Stop()
		menubar.Forget()
		fmt.Println("off — the menu bar icon is gone")
		return st.SaveConfig()
	case len(args) > 0:
		return fmt.Errorf("usage: agtop menubar [off]")
	}
	fmt.Println("starting the menu bar icon (the first time builds it, a few seconds)…")
	if err := menubar.Start(); err != nil {
		return err
	}
	st.Config.MenuBar, st.Config.MenuBarAsked = true, true
	fmt.Println("on — agtop is in your menu bar. Its menu can open it at login.")
	return st.SaveConfig()
}

// selfUpdate is agtop update: the newest agtop over this one, when there is
// a newer one.
func selfUpdate() error {
	ctx := context.Background()
	l, err := update.Latest(ctx)
	if err != nil {
		return err
	}
	if cur, ok := update.Current(); ok && !update.Newer(ctx, cur, l) {
		fmt.Println("agtop", cur.Short(), "is the newest")
		return nil
	}
	fmt.Println("installing agtop", l.Short()+"…")
	to, err := update.Install(ctx)
	if err != nil {
		return err
	}
	fmt.Println("agtop", to.Short(), "installed · reopen agtop to use it")
	return nil
}
