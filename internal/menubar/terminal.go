package menubar

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Clicking a notification, or an agent in the menu, comes back to agtop in
// the terminal it's open in (Warp, iTerm, Ghostty…), on that agent: each
// agtop leaves its terminal's bundle ID under open/<pid> while it runs, and
// picks up an agent to go to from open/<pid>.goto.

func openDir() string      { return filepath.Join(dir(), "open") }
func lastTerminal() string { return filepath.Join(dir(), "terminal") }

// Here marks this agtop as open in its terminal until the returned func is
// called.
func Here() func() {
	id := terminalID()
	if runtime.GOOS != "darwin" || id == "" {
		return func() {}
	}
	p := filepath.Join(openDir(), strconv.Itoa(os.Getpid()))
	if os.MkdirAll(openDir(), 0o700) != nil || os.WriteFile(p, []byte(id), 0o600) != nil {
		return func() {}
	}
	_ = os.WriteFile(lastTerminal(), []byte(id), 0o600)
	return func() {
		_ = os.Remove(p)
		_ = os.Remove(p + ".goto")
	}
}

// Goto is the agent the menu bar app last asked this agtop to go to, once.
func Goto() string {
	p := filepath.Join(openDir(), strconv.Itoa(os.Getpid())+".goto")
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	_ = os.Remove(p)
	return strings.TrimSpace(string(b))
}

// terminalID is the bundle ID of the app agtop's terminal is: macOS hands
// it to everything an app starts, tmux included; TERM_PROGRAM names the
// usual ones when it didn't.
func terminalID() string {
	if id := os.Getenv("__CFBundleIdentifier"); id != "" && id != BundleID {
		return id
	}
	return map[string]string{
		"Apple_Terminal": "com.apple.Terminal",
		"iTerm.app":      "com.googlecode.iterm2",
		"WarpTerminal":   "dev.warp.Warp-Stable",
		"ghostty":        "com.mitchellh.ghostty",
		"WezTerm":        "com.github.wez.wezterm",
		"vscode":         "com.microsoft.VSCode",
	}[os.Getenv("TERM_PROGRAM")]
}

// open is the agtop open most recently, and the terminal it's in.
func open() (pid int, id string) {
	ents, _ := os.ReadDir(openDir())
	var at time.Time
	for _, e := range ents {
		n, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		p := filepath.Join(openDir(), e.Name())
		if errors.Is(syscall.Kill(n, 0), syscall.ESRCH) {
			_ = os.Remove(p)
			_ = os.Remove(p + ".goto")
			continue
		}
		info, err := e.Info()
		b, _ := os.ReadFile(p)
		if err == nil && len(b) > 0 && info.ModTime().After(at) {
			pid, id, at = n, string(b), info.ModTime()
		}
	}
	return pid, id
}

// Show brings agtop forward on the agent key (if any): the terminal it's
// open in, or, with none open, a new agtop in the terminal it last was.
func Show(key string) error {
	if pid, id := open(); pid != 0 {
		if key != "" {
			_ = os.WriteFile(filepath.Join(openDir(), strconv.Itoa(pid)+".goto"), []byte(key), 0o600)
		}
		return exec.Command("/usr/bin/open", "-b", id).Run()
	}
	bin := exe()
	if b, _ := os.ReadFile(lastTerminal()); runsFiles(string(b)) {
		if exec.Command("/usr/bin/open", "-b", string(b), bin).Run() == nil {
			return nil
		}
	}
	return exec.Command("/usr/bin/open", bin).Run()
}

// runsFiles says whether the terminal runs a program it's asked to open;
// the others don't take one this way, so a new agtop opens in Terminal.
func runsFiles(id string) bool {
	return id == "com.apple.Terminal" || id == "com.googlecode.iterm2" || strings.HasPrefix(id, "dev.warp.Warp")
}
