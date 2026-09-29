// Command shots draws the README's screenshots from a made-up world:
// agents, accounts, repositories and processes that belong to nobody. It
// runs rush's own view without a terminal, takes each screen's frame and
// draws it as a picture.
//
//	go -C tools/shots run . ../../docs/screenshots
//
// It writes WebP with cwebp when it's installed, else PNG. The world is
// built afresh in /tmp/rush-shots each run.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/state"
	"github.com/0xdeafcafe/rush/internal/ui"
)

// root is where the world is built: short, as unix socket paths must be,
// and with no link on the way, as the kernel names folders.
const root = "/private/tmp/rush-shots"

// shot is one screenshot: its name, the terminal's size, and the keys
// that reach its screen.
type shot struct {
	name string
	w, h int
	keys []string
	// prep is done to the world before the view starts.
	prep func(*world)
}

func main() {
	only := flag.String("only", "", "draw only the shots whose names contain this")
	scale := flag.Float64("scale", 2, "pixels per point")
	keepPNG := flag.Bool("png", false, "write PNG even when cwebp is installed")
	flag.Parse()
	out := "../../docs/screenshots"
	if flag.NArg() > 0 {
		out = flag.Arg(0)
	}
	out, _ = filepath.Abs(out)
	if err := run(out, *only, *scale, *keepPNG); err != nil {
		fmt.Fprintln(os.Stderr, "shots:", err)
		os.Exit(1)
	}
}

func run(out, only string, scale float64, keepPNG bool) error {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	fs, err := loadFonts(13 * scale)
	if err != nil {
		return err
	}
	cwebp, _ := exec.LookPath("cwebp")
	if keepPNG {
		cwebp = ""
	}
	w, err := build(root)
	if err != nil {
		return err
	}
	defer w.close()
	for _, s := range shots {
		if only != "" && !strings.Contains(s.name, only) {
			continue
		}
		if s.prep != nil {
			s.prep(w)
		}
		frame := capture(s)
		if bad := leaks(plain(frame)); bad != "" {
			return fmt.Errorf("%s: the frame has %q in it", s.name, bad)
		}
		_ = os.WriteFile(filepath.Join(root, s.name+".txt"), []byte(plain(frame)), 0o644)
		img := window(fs.paint(screen(frame, s.w, s.h), s.w, s.h), "rush", fs, scale)
		if err := save(img, out, s.name, cwebp); err != nil {
			return err
		}
		fmt.Println("drew", s.name)
	}
	if cwebp == "" && !keepPNG {
		fmt.Println("cwebp isn't installed, so these are PNG: brew install webp")
	}
	return nil
}

// capture runs the view at the shot's size, presses its keys and takes
// the frame.
func capture(s shot) string {
	m := ui.New(state.Load(), "1.0.0")
	m.Offline()
	d := drive(m, s.w, s.h)
	// What a dark terminal answers when rush asks for its colours.
	d.update(tea.BackgroundColorMsg{Color: termBG})
	d.update(tea.ForegroundColorMsg{Color: termFG})
	d.settle(4 * time.Second)
	for _, k := range s.keys {
		switch {
		case strings.HasPrefix(k, "text="):
			d.press(typed(k[5:])...)
		case strings.HasPrefix(k, "wait="):
			t, _ := time.ParseDuration(k[5:])
			d.settle(t)
		default:
			d.press(key(k))
		}
	}
	d.settle(2 * time.Second)
	return d.frame()
}

func save(img image.Image, dir, name, cwebp string) error {
	p := filepath.Join(root, name+".png")
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	f.Close()
	if cwebp == "" {
		return copyFile(p, filepath.Join(dir, name+".png"))
	}
	b, err := exec.Command(cwebp, "-quiet", "-q", "90", "-alpha_q", "100", p, "-o", filepath.Join(dir, name+".webp")).CombinedOutput()
	if err != nil {
		return fmt.Errorf("cwebp: %v: %s", err, b)
	}
	return nil
}

func copyFile(from, to string) error {
	b, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, b, 0o644)
}

// leaks is anything of the machine's own that made it into a frame: its
// user's name or home, or the maintainer's name and work.
func leaks(text string) string {
	home, _ := os.UserHomeDir()
	lower := strings.ToLower(text)
	for _, s := range []string{realHome, realUser, home, "/users/", "alex", "forbes", "langwatch"} {
		if s != "" && s != "/" && strings.Contains(lower, strings.ToLower(s)) && !strings.HasPrefix(s, root) {
			return s
		}
	}
	return ""
}
