package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// #mackeys: Terminal.app keeps ⌘ for its own menus, so ⌘← and the like
// never reach agtop there. Opted into, Hammerspoon sends them on as the
// ctrl keys agtop and the shell understand, and only while Terminal.app is
// in front.

// inAppleTerminal is whether agtop is drawing in macOS's own Terminal.app.
func inAppleTerminal() bool {
	return runtime.GOOS == "darwin" && os.Getenv("TERM_PROGRAM") == "Apple_Terminal"
}

// hsRequire is the line agtop adds to Hammerspoon's init.lua.
const hsRequire = `require("agtop") -- agtop's ⌘ keys for Terminal.app; #mackeys off removes this`

// hsScript is ~/.hammerspoon/agtop.lua.
const hsScript = `-- Written by agtop (#mackeys); #mackeys off removes it, and agtop
-- overwrites any change. Terminal.app keeps ⌘ for its menus, so while it is
-- in front these ⌘ keys are sent as the ctrl keys a terminal understands.
local M = {}

local remap = {
  { { "cmd" }, "left", { "ctrl" }, "a" },          -- line start
  { { "cmd" }, "right", { "ctrl" }, "e" },         -- line end
  { { "cmd" }, "delete", { "ctrl" }, "u" },        -- clear to line start
  { { "cmd" }, "forwarddelete", { "ctrl" }, "k" }, -- clear to line end (fn+⌘⌫)
  { { "cmd" }, "z", { "ctrl" }, "-" },             -- undo
  { { "cmd", "shift" }, "z", { "ctrl" }, "y" },    -- redo
}

local codes = hs.keycodes.map

-- same is whether flags hold exactly mods; fn, set on every arrow, is ignored.
local function same(flags, mods)
  local want = {}
  for _, m in ipairs(mods) do want[m] = true end
  for _, m in ipairs({ "cmd", "alt", "ctrl", "shift" }) do
    if (flags[m] or false) ~= (want[m] or false) then return false end
  end
  return true
end

M.tap = hs.eventtap.new({ hs.eventtap.event.types.keyDown }, function(e)
  local code, flags = e:getKeyCode(), e:getFlags()
  for _, r in ipairs(remap) do
    if code == codes[r[2]] and same(flags, r[1]) then
      local app = hs.application.frontmostApplication()
      if not app or app:bundleID() ~= "com.apple.Terminal" then return false end
      return true, {
        hs.eventtap.event.newKeyEvent(r[3], r[4], true),
        hs.eventtap.event.newKeyEvent(r[3], r[4], false),
      }
    end
  end
  return false
end):start()

return M
`

func hsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".hammerspoon")
}

// hammerspoonApp is where Hammerspoon is installed, or "".
func hammerspoonApp() string {
	home, _ := os.UserHomeDir()
	for _, p := range []string{"/Applications/Hammerspoon.app", filepath.Join(home, "Applications", "Hammerspoon.app")} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// macKeysOn is whether Hammerspoon is set to load agtop's keys.
func macKeysOn() bool {
	init, err := os.ReadFile(filepath.Join(hsDir(), "init.lua"))
	if err != nil || !strings.Contains(string(init), hsRequire) {
		return false
	}
	_, err = os.Stat(filepath.Join(hsDir(), "agtop.lua"))
	return err == nil
}

// macKeysCommand is #mackeys.
func (m *Model) macKeysCommand(arg string) tea.Cmd {
	if runtime.GOOS != "darwin" {
		m.flash("#mackeys is for macOS's Terminal.app", true)
		return nil
	}
	switch strings.TrimSpace(arg) {
	case "on":
		m.didStep("mackeys")
		if hammerspoonApp() == "" {
			m.flash("installing Hammerspoon with brew…", false)
		} else {
			m.flash("setting up ⌘ keys for Terminal.app…", false)
		}
		return func() tea.Msg {
			if err := macKeysInstall(); err != nil {
				return doneMsg{err: err}
			}
			return doneMsg{text: "⌘ keys on in Terminal.app: ⌘← → line ends · ⌘⌫ ⌘⌦ clear to them · ⌘Z undo · allow Hammerspoon in Accessibility if macOS asks"}
		}
	case "off":
		m.didStep("mackeys") // a no is an answer too
		return func() tea.Msg {
			if err := macKeysRemove(); err != nil {
				return doneMsg{err: err}
			}
			return doneMsg{text: "⌘ keys off · Hammerspoon stays installed"}
		}
	}
	if macKeysOn() {
		m.flash("⌘ keys are on in Terminal.app, through Hammerspoon · #mackeys off", false)
	} else {
		m.flash("⌘ keys are off · #mackeys on sends ⌘← → ⌘⌫ ⌘⌦ ⌘Z on through Hammerspoon", false)
	}
	return nil
}

// macKeysInstall installs Hammerspoon if it isn't, writes agtop's keys into
// its config and restarts it so they load.
func macKeysInstall() error {
	if hammerspoonApp() == "" {
		brew, err := exec.LookPath("brew")
		if err != nil {
			return errors.New("#mackeys needs Hammerspoon: install it from hammerspoon.org, then #mackeys on")
		}
		if out, err := exec.Command(brew, "install", "--cask", "hammerspoon").CombinedOutput(); err != nil {
			return fmt.Errorf("installing Hammerspoon: %s", lastLine(string(out), err))
		}
	}
	dir := hsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "agtop.lua"), []byte(hsScript), 0o644); err != nil {
		return err
	}
	initPath := filepath.Join(dir, "init.lua")
	init, err := os.ReadFile(initPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if !strings.Contains(string(init), hsRequire) {
		s := string(init)
		if s != "" && !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		if err := os.WriteFile(initPath, []byte(s+hsRequire+"\n"), 0o644); err != nil {
			return err
		}
	}
	return restartHammerspoon(true)
}

// macKeysRemove takes agtop's keys back out of Hammerspoon's config.
func macKeysRemove() error {
	dir := hsDir()
	initPath := filepath.Join(dir, "init.lua")
	if init, err := os.ReadFile(initPath); err == nil && strings.Contains(string(init), hsRequire) {
		s := strings.Replace(string(init), hsRequire+"\n", "", 1)
		s = strings.Replace(s, hsRequire, "", 1)
		if err := os.WriteFile(initPath, []byte(s), 0o644); err != nil {
			return err
		}
	}
	if err := os.Remove(filepath.Join(dir, "agtop.lua")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return restartHammerspoon(false)
}

// restartHammerspoon quits Hammerspoon if it's running, so it reads its
// config again, and opens it in the background; start opens it even when
// it wasn't running.
func restartHammerspoon(start bool) error {
	running := exec.Command("pgrep", "-xq", "Hammerspoon").Run() == nil
	if running {
		_ = exec.Command("osascript", "-e", `tell application "Hammerspoon" to quit`).Run()
		for i := 0; i < 30 && exec.Command("pgrep", "-xq", "Hammerspoon").Run() == nil; i++ {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if !running && !start {
		return nil
	}
	if out, err := exec.Command("open", "-g", "-a", "Hammerspoon").CombinedOutput(); err != nil {
		return fmt.Errorf("opening Hammerspoon: %s", lastLine(string(out), err))
	}
	return nil
}

// lastLine is the last line a command printed, or its error.
func lastLine(out string, err error) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if l := strings.TrimSpace(lines[len(lines)-1]); l != "" {
		return l
	}
	return err.Error()
}
