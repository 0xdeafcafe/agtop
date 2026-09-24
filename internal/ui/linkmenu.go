package ui

import (
	"errors"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/cellw"
)

// linkAct is one thing the menu on a link can do with it.
type linkAct struct {
	label string
	do    func(m *Model) tea.Cmd
}

// previewable are the files Preview opens; the rest get Quick Look.
var previewable = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".heic": true,
	".tif": true, ".tiff": true, ".bmp": true, ".pdf": true, ".psd": true, ".ico": true,
}

// linkAt is the URL of the link drawn at cell col of a styled row, or
// nothing where there's none.
func linkAt(s string, col int) string {
	at, u := 0, ""
	for {
		i := strings.Index(s, "\x1b]8;;")
		seg := s
		if i >= 0 {
			seg = s[:i]
		}
		w := cellw.String(ansi.Strip(seg))
		if u != "" && col >= at && col < at+w {
			return u
		}
		at += w
		if i < 0 || col < at {
			return ""
		}
		s = s[i+len("\x1b]8;;"):]
		j := strings.Index(s, "\x1b\\")
		if j < 0 {
			return ""
		}
		u, s = s[:j], s[j+2:]
	}
}

// linkMenu opens the menu for the link under a right-click in the pane,
// reporting whether there was one.
func (m *Model) linkMenu(c *hostConn, x, y int) bool {
	i := y - m.paneTop
	if i < 0 || i >= len(c.rowBody) || c.rowBody[i] < 0 || m.viewName(c) == "screen" {
		return false
	}
	at, ok := m.textCell(c, x, y)
	if !ok || at.row >= len(c.shown) {
		return false
	}
	target := linkAt(c.shown[at.row].Text, at.col)
	if target == "" {
		return false
	}
	acts := linkActs(target)
	if acts == nil {
		return false
	}
	title := target
	if u, err := url.Parse(target); err == nil && u.Scheme == "file" {
		title = tildify(u.Path)
	}
	m.picker = &picker{title: title, acts: acts}
	return true
}

// linkActs are what can be done with a link: a file can be opened, in
// Preview when it's an image or PDF, looked at, found or copied; a web
// address opened or copied.
func linkActs(target string) []linkAct {
	u, err := url.Parse(target)
	if err != nil {
		return nil
	}
	if u.Scheme != "file" {
		return []linkAct{
			{"Open in browser", func(*Model) tea.Cmd { return browse(target) }},
			{"Copy link", func(m *Model) tea.Cmd { m.copyText(target); return nil }},
		}
	}
	p := u.Path
	acts := []linkAct{{"Open", func(*Model) tea.Cmd { return run("opened "+filepath.Base(p), "open", p) }}}
	if previewable[strings.ToLower(filepath.Ext(p))] {
		acts = append(acts, linkAct{"Open with Preview", func(*Model) tea.Cmd {
			return run("opened "+filepath.Base(p)+" in Preview", "open", "-a", "Preview", p)
		}})
	}
	return append(acts,
		linkAct{"Quick Look", func(*Model) tea.Cmd { return quickLook(p) }},
		linkAct{"Reveal in Finder", func(*Model) tea.Cmd { return run("revealed "+filepath.Base(p), "open", "-R", p) }},
		linkAct{"Copy path", func(m *Model) tea.Cmd { m.copyText(p); return nil }},
	)
}

// run runs a command, saying done when it's done or why it couldn't.
func run(done string, name string, args ...string) tea.Cmd {
	return func() tea.Msg {
		if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
			if s := strings.TrimSpace(string(out)); s != "" {
				return doneMsg{err: errors.New(s)}
			}
			return doneMsg{err: err}
		}
		return doneMsg{text: done}
	}
}

// quickLook shows a file in a Quick Look panel. qlmanage stays until the
// panel is closed, so it's left to run.
func quickLook(p string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("qlmanage", "-p", p)
		if err := cmd.Start(); err != nil {
			return doneMsg{err: err}
		}
		go cmd.Wait()
		return nil
	}
}
