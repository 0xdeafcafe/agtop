package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// clipImageMsg is an image read off the clipboard and saved to a file;
// path is empty when the clipboard held none.
type clipImageMsg struct {
	path string
	err  error
}

// pasteClipImage reads an image off the clipboard, as ctrl+v does in Claude
// Code: a terminal pastes only text, so a screenshot copied to the clipboard
// never reaches agtop as a paste.
func pasteClipImage() tea.Cmd {
	return func() tea.Msg {
		path, err := clipImage()
		return clipImageMsg{path: path, err: err}
	}
}

// clipImage is the image on the clipboard as a file: an image file copied in
// the Finder as itself, image data saved as a PNG under the temp dir. It is
// "" with no error when the clipboard holds no image.
func clipImage() (string, error) {
	dir := filepath.Join(os.TempDir(), "agtop-images")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	out := filepath.Join(dir, fmt.Sprintf("clipboard-%s.png", time.Now().Format("20060102-150405.000")))
	switch runtime.GOOS {
	case "darwin":
		// A copied file: attach the file itself when it's an image.
		if b, err := exec.Command("osascript", "-e", "POSIX path of (the clipboard as «class furl»)").Output(); err == nil {
			if p := imagePath(strings.TrimSpace(string(b))); p != "" {
				return p, nil
			}
		}
		script := []string{
			"-e", "set png to (the clipboard as «class PNGf»)",
			"-e", fmt.Sprintf("set f to open for access POSIX file %q with write permission", out),
			"-e", "write png to f",
			"-e", "close access f",
		}
		if exec.Command("osascript", script...).Run() != nil {
			os.Remove(out)
			return "", nil // no image data on the clipboard
		}
	default:
		var data []byte
		var err error
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			data, err = exec.Command("wl-paste", "--type", "image/png").Output()
		} else {
			data, err = exec.Command("xclip", "-selection", "clipboard", "-t", "image/png", "-o").Output()
		}
		if err != nil || len(data) == 0 {
			return "", nil
		}
		if err := os.WriteFile(out, data, 0o600); err != nil {
			return "", err
		}
	}
	if st, err := os.Stat(out); err != nil || st.Size() == 0 {
		os.Remove(out)
		return "", nil
	}
	return out, nil
}

// attachImages adds image files to whichever box has focus: in a
// Session's box as [Image #N] at the cursor, in the Prompt as chips.
func (m *Model) attachImages(imgs []string) {
	if c := m.host; c != nil && m.paneFocus {
		// Each goes in the text as its marker, at the cursor.
		var marks []string
		for _, p := range imgs {
			marks = append(marks, c.imgs.add(p))
		}
		pos := max(0, len(c.input)-c.back)
		c.undo.save(c.input, c.back, false)
		ins := strings.Join(marks, " ")
		if pos > 0 && c.input[pos-1] != ' ' && c.input[pos-1] != '\n' {
			ins = " " + ins
		}
		if pos < len(c.input) && c.input[pos] != ' ' {
			ins += " "
		}
		c.input = insert(c.input, pos, []rune(ins))
		return
	}
	m.images = append(m.images, imgs...)
	m.flash(fmt.Sprintf("attached %d image(s)", len(imgs)), false)
}
