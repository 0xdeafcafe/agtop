package host

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent/event"
	"github.com/0xdeafcafe/agtop/internal/headless"
)

// A client that says it reads events gets a Claude Code session as agtop's
// own; one that says nothing, an older agtop, gets Claude Code's lines,
// and the op it sends first is still carried out.
func TestHelloChoosesEncoding(t *testing.T) {
	bin := setup(t)
	cfg, err := Spawn(Config{Cwd: filepath.Dir(bin), Binary: bin, IdleStop: Duration(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	old, err := dial(cfg.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	if err := old.Send("hi"); err != nil { // before any hello wait is over
		t.Fatal(err)
	}
	next(t, old, func(ev any) bool { _, ok := ev.(headless.PermissionRequest); return ok })

	c, err := dial(cfg.ID, eventsFrom)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	timeout := time.After(5 * time.Second)
	var sawCall bool
	for !sawCall {
		select {
		case line, ok := <-c.Lines:
			if !ok {
				t.Fatal("connection closed")
			}
			if isClaudeLine(line) {
				t.Fatalf("a client reading events got Claude Code's line %s", line)
			}
			if !bytes.HasPrefix(line, []byte(`{"type":"`+typeEvent+`"`)) {
				continue
			}
			ev, err := Decode(line)
			if err != nil {
				t.Fatalf("decode %s: %v", line, err)
			}
			if m, ok := ev.(event.Message); ok {
				for _, p := range m.Parts {
					sawCall = sawCall || p.Call != nil && p.Call.Input.Command == "echo hi"
				}
			}
		case <-timeout:
			t.Fatal("no call came as an event")
		}
	}
}
