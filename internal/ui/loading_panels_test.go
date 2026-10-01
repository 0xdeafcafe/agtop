package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/adapters/claude/headless"
	"github.com/0xdeafcafe/rush/internal/convo"
	"github.com/charmbracelet/x/ansi"
)

func TestLoadingPanelsStayAtBottomUntilReplay(t *testing.T) {
	for _, size := range [][2]int{{60, 30}, {120, 50}} {
		m, _ := benchModel(200, 55)
		c := m.host
		c.sess = convo.New()
		c.ready = false
		c.input = nil
		c.subs = []convo.Subagent{{ID: "child", Type: "loading-worker", Mod: time.Now().UnixNano()}}
		addJob := func() {
			c.sess.Apply(headless.TaskStarted{ID: "loading-job", Type: "local_bash", Description: "background-checks", Backgrounded: true}, time.Now())
		}
		addJob()
		c.sess.Info.Queue = []string{"queued-during-load"}
		var dock, box, worker int
		for pass := 0; pass < 3; pass++ {
			rows := m.rushPane(size[0], size[1])
			if len(rows) > size[1] {
				t.Fatalf("%v: pane overflow", size)
			}
			if !strings.Contains(ansi.Strip(rows[c.bodyTop]), "loading recent messages") {
				t.Fatal("loading message is not flush below header")
			}
			worker = -1
			for i, row := range rows {
				if strings.Contains(ansi.Strip(row), "loading-worker") {
					worker = i
					break
				}
			}
			if worker < c.dockY-m.paneTop || worker <= c.bodyTop+1 {
				t.Fatalf("%v: loading panels jumped to row %d; dock %d", size, worker, c.dockY)
			}
			text := ansi.Strip(strings.Join(rows, "\n"))
			if !strings.Contains(text, "background-checks") || !strings.Contains(text, "queued-during-load") {
				t.Fatalf("loading lost background/queue panels:\n%s", text)
			}
			if pass > 0 && (dock != c.dockY || box != c.boxY) {
				t.Fatal("loading redraw moved panels/composer")
			}
			dock, box = c.dockY, c.boxY
			for row := range c.qAt {
				if y := c.dockY + c.qTop + row - m.paneTop; y < 0 || y >= len(rows) || !strings.Contains(ansi.Strip(rows[y]), "queued-during-load") {
					t.Fatal("queue hit target lost during loading")
				}
			}
		}
		c.sess = benchConvo(30)
		c.sess.Info.Queue = []string{"queued-during-load"}
		addJob()
		c.ready = true
		rows := m.rushPane(size[0], size[1])
		if c.boxY != box {
			t.Fatal("replay moved composer")
		}
		for i, row := range rows {
			if strings.Contains(ansi.Strip(row), "loading-worker") && i != worker {
				t.Fatalf("replay moved worker from %d to %d", worker, i)
			}
		}
		if strings.Contains(ansi.Strip(strings.Join(rows, "\n")), "loading recent messages") {
			t.Fatal("placeholder survived replay")
		}
		c.scroll = c.auxRows + 10
		c.scrollOnly = true
		text := ansi.Strip(strings.Join(m.rushPane(size[0], size[1]), "\n"))
		if strings.Contains(text, "background-checks") || strings.Contains(text, "queued-during-load") {
			t.Fatal("loaded panels no longer scroll with conversation")
		}
	}
}
