package harnessup

import (
	"path/filepath"
	"strings"

	"github.com/0xdeafcafe/rush/internal/agent"
)

// Update is how to bring a harness up to date: Label for the flash and
// the page, Argv to run (the first word is found as rush finds agents).
type Update struct {
	Label string
	Argv  []string
}

// source is where an install came from, read from its real path.
type source struct {
	brew, cask string // a Homebrew formula or cask's name
	npm        string // the npm package it sits in
	manager    string // the node package manager that put it there
	uv, pipx   string // the Python tool it is
}

// sourceOf reads where a program at real (its path past symlinks and
// rush's stand-ins) was installed from.
func sourceOf(real string) source {
	var s source
	parts := strings.Split(filepath.ToSlash(real), "/")
	after := func(dir string) (string, bool) {
		for i, p := range parts {
			if p == dir && i+1 < len(parts) {
				return parts[i+1], true
			}
		}
		return "", false
	}
	if n, ok := after("Caskroom"); ok {
		s.cask = n
	}
	if n, ok := after("Cellar"); ok {
		s.brew = n
	}
	if n, ok := after("node_modules"); ok {
		if strings.HasPrefix(n, "@") {
			for i, p := range parts {
				if p == "node_modules" && i+2 < len(parts) {
					n += "/" + parts[i+2]
				}
			}
		}
		s.npm = n
		switch {
		case strings.Contains(real, "/.bun/"):
			s.manager = "bun"
		case strings.Contains(real, "pnpm"):
			s.manager = "pnpm"
		default:
			s.manager = "npm"
		}
	}
	if n, ok := after("tools"); ok && strings.Contains(real, "/uv/tools/") {
		s.uv = n
	}
	if n, ok := after("venvs"); ok && strings.Contains(real, "/pipx/venvs/") {
		s.pipx = n
	}
	return s
}

// planFor is how to update the program at path (real is where it
// resolves): brew when brew installed it, else the program's own
// updater, else the package manager it sits under. Empty when none.
func planFor(pub agent.Published, path string, s source) Update {
	switch {
	case s.cask != "":
		return Update{"brew upgrade --cask " + s.cask, []string{"brew", "upgrade", "--cask", s.cask}}
	case s.brew != "":
		return Update{"brew upgrade " + s.brew, []string{"brew", "upgrade", s.brew}}
	case len(pub.Update) > 0:
		name := filepath.Base(path)
		return Update{name + " " + strings.Join(pub.Update, " "), append([]string{path}, pub.Update...)}
	case s.npm != "":
		pkg := s.npm + "@latest"
		if s.manager == "npm" {
			return Update{"npm install -g " + pkg, []string{"npm", "install", "-g", pkg}}
		}
		return Update{s.manager + " add -g " + pkg, []string{s.manager, "add", "-g", pkg}}
	case s.uv != "":
		return Update{"uv tool upgrade " + s.uv, []string{"uv", "tool", "upgrade", s.uv}}
	case s.pipx != "":
		return Update{"pipx upgrade " + s.pipx, []string{"pipx", "upgrade", s.pipx}}
	}
	return Update{}
}
