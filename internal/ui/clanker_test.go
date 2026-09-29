package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestClankerShape(t *testing.T) {
	for md := moodIdle; md <= moodSleepy; md++ {
		for tick := 0; tick < 90; tick++ {
			fx := clkFX{kind: fxKind(tick % 8), frame: tick % 16}
			lines := clanker(clkState{md: md, tick: tick, fx: fx, rich: true})
			if len(lines) < 3 || len(lines) > clkH {
				t.Fatalf("mood %d tick %d: %d lines", md, tick, len(lines))
			}
			w := ansi.StringWidth(lines[0])
			if w != clkW {
				t.Fatalf("mood %d tick %d: width %d, want clkW", md, tick, w)
			}
			for i, l := range lines {
				if got := ansi.StringWidth(l); got != w {
					t.Fatalf("mood %d tick %d line %d: width %d, want %d", md, tick, i, got, w)
				}
			}
		}
	}
}

func TestClankerSheet(t *testing.T) {
	out := os.Getenv("CLK_SHEET")
	if out == "" {
		t.Skip("set CLK_SHEET to a file path to render the contact sheet")
	}
	type strip struct {
		name   string
		frames []clkState
	}
	strips := make([]strip, 0, 9)
	for md, name := range []string{"idle", "working", "needs you", "sleepy"} {
		st := strip{name: name}
		for tick := range 6 {
			st.frames = append(st.frames, clkState{md: mood(md), tick: tick})
		}
		strips = append(strips, st)
	}
	for _, r := range []struct {
		name string
		kind fxKind
		md   mood
	}{{"shimmer", fxShimmer, moodIdle}, {"asked", fxAsk, moodNeedsYou}, {"answered", fxAnswered, moodWorking},
		{"finished", fxDone, moodIdle}, {"error", fxError, moodIdle}} {
		st := strip{name: r.name}
		for f := 0; f < fxLen[r.kind]; f += max(1, fxLen[r.kind]/6) {
			st.frames = append(st.frames, clkState{md: r.md, tick: 1, fx: clkFX{kind: r.kind, frame: f}})
		}
		strips = append(strips, st)
	}
	var sb strings.Builder
	for _, st := range strips {
		frames := make([][]string, len(st.frames))
		for i, s := range st.frames {
			frames[i] = clanker(s)
		}
		for row := range frames[0] {
			label := "          "
			if row == 2 {
				label = fit(dim(st.name), 10)
			}
			sb.WriteString(label)
			for _, f := range frames {
				sb.WriteString(f[row] + "   ")
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}
	if err := os.WriteFile(out, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// clkScript is a minute of clanker's life, second by second: what he's
// doing and what happens.
var clkScript = []struct {
	at    int
	md    mood
	event fxKind
	say   string
}{
	{40, moodWorking, fxNone, "working"},
	{52, moodWorking, fxAsk, "an agent asks you something"},
	{53, moodNeedsYou, fxNone, "needs you"},
	{58, moodWorking, fxAnswered, "you answer it"},
	{62, moodWorking, fxDone, "an agent finishes"},
	{66, moodWorking, fxError, "an agent hits an API error"},
	{70, moodIdle, fxNone, "idle"},
	{78, moodSleepy, fxNone, "asleep"},
	{84, moodSleepy, fxNone, ""},
}

// clkPlay runs the script at real speed, calling draw with each frame.
func clkPlay(draw func(lines []string, say string, at time.Duration)) {
	m := &Model{tick: clkScript[0].at}
	start := time.Now()
	var md mood
	say := ""
	next := time.Duration(0)
	for i := 0; ; {
		now := time.Since(start)
		sec := clkScript[0].at + int(now/time.Second)
		for ; i < len(clkScript) && clkScript[i].at <= sec; i++ {
			md, say = clkScript[i].md, clkScript[i].say
			if clkScript[i].event != fxNone {
				m.react(clkScript[i].event)
			}
		}
		if i == len(clkScript) {
			return
		}
		if sec != m.tick {
			m.tick = sec
			m.clkBeat(md)
		}
		if m.fxOn && now >= next {
			m.onFXTick()
			next = now + fxEvery
		}
		draw(clanker(clkState{md: md, tick: m.tick, fx: m.fx, rich: true}), say, now)
		time.Sleep(10 * time.Millisecond)
	}
}

// TestClankerPlay plays clanker's minute in the terminal:
// CLK_PLAY=1 go test ./internal/ui -run ClankerPlay
func TestClankerPlay(t *testing.T) {
	if os.Getenv("CLK_PLAY") == "" {
		t.Skip("set CLK_PLAY=1 to watch him")
	}
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		t.Skip("no terminal")
	}
	defer tty.Close()
	fmt.Fprint(tty, "\x1b[?25l\n\n\n\n\n")
	clkPlay(func(lines []string, say string, _ time.Duration) {
		fmt.Fprint(tty, "\x1b[5A")
		for i, l := range lines {
			s := ""
			if i == 1 {
				s = dim(say)
			}
			fmt.Fprintf(tty, "\r  %s   %s\x1b[K\n", l, s)
		}
		fmt.Fprint(tty, "\x1b[K\n")
	})
	fmt.Fprint(tty, "\x1b[?25h")
}
