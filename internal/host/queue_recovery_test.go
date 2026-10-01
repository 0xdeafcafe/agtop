package host

import (
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent/event"
)

func TestQueueSurvivesHostRestart(t *testing.T) {
	for _, tc := range []struct {
		name        string
		crash, held bool
	}{{"restart", false, true}, {"crash", true, true}, {"automatic", true, false}} {
		name, crash := tc.name, tc.crash
		t.Run(name, func(t *testing.T) {
			bin := setup(t)
			cfg, err := Spawn(Config{Cwd: filepath.Dir(bin), Prompt: "initial", Binary: bin})
			if err != nil {
				t.Fatal(err)
			}
			c, err := Dial(cfg.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			next(t, c, inState("blocked"))
			if err := c.HoldQueue(tc.held); err != nil {
				t.Fatal(err)
			}
			if err := c.QueueSeparately(true); err != nil {
				t.Fatal(err)
			}
			pic := filepath.Join(filepath.Dir(bin), "attachment.jpg")
			if err := os.WriteFile(pic, []byte("image"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := c.SendImages("first queued", []string{pic}, false); err != nil {
				t.Fatal(err)
			}
			if err := c.SendExchange(event.Exchange{Text: "second queued"}, false); err != nil {
				t.Fatal(err)
			}
			next(t, c, func(ev any) bool { i, ok := ev.(InfoEvent); return ok && len(i.Info.Queue) == 2 })
			before, err := ReadInfo(cfg.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(before.Queue) != 2 {
				t.Fatalf("before: %+v", before.Queue)
			}
			if crash {
				if err := syscall.Kill(before.HostPID, syscall.SIGKILL); err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(5 * time.Second)
				for alive(before.HostPID) && time.Now().Before(deadline) {
					time.Sleep(10 * time.Millisecond)
				}
				if err := Ensure(cfg.ID); err != nil {
					t.Fatal(err)
				}
			} else if err := Restart(cfg.ID, func(*Config) {}); err != nil {
				t.Fatal(err)
			}
			after, err := ReadInfo(cfg.ID)
			if err != nil {
				t.Fatal(err)
			}
			if tc.held && (!reflect.DeepEqual(after.Queue, before.Queue) || !reflect.DeepEqual(after.QueueImages, before.QueueImages) || !reflect.DeepEqual(after.QueueExchanges, before.QueueExchanges) || !after.QueueHeld || !after.QueueSeparate) {
				t.Fatalf("queue changed across %s: before %+v; after %+v", name, before, after)
			}
			resumed, err := Dial(cfg.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer resumed.Close()
			if err := resumed.HoldQueue(false); err != nil {
				t.Fatal(err)
			}
			sent := next(t, resumed, func(ev any) bool { i, ok := ev.(InfoEvent); return ok && len(i.Info.Queue) == 1 }).(InfoEvent).Info
			if sent.Queue[0] != "second queued" {
				t.Fatalf("wrong order: %v", sent.Queue)
			}
		})
	}
}

func TestQueueRecoveryRejectsOtherConversation(t *testing.T) {
	old := Info{ID: "one", Kind: "kimi", SessionID: "original", Queue: []string{"keep"}}
	for _, cfg := range []Config{{ID: "two", Kind: "kimi", SessionID: "original"}, {ID: "one", Kind: "codex", SessionID: "original"}, {ID: "one", Kind: "kimi", SessionID: "other"}, {ID: "one", Kind: "kimi", SessionID: "original", Fork: true}} {
		s := &server{cfg: cfg}
		s.restoreQueue(old)
		if len(s.info.Queue) > 0 {
			t.Fatalf("carried queue into %+v", cfg)
		}
	}
}
