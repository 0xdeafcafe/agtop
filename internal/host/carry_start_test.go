package host

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
)

// Carry without a prompt must start and initialize the destination, rather
// than merely publish an idle socket that will fail on the first send.
func TestCarriedHostInitializesWithoutPrompt(t *testing.T) {
	home := filepath.Dir(setup(t))
	cfg, err := Spawn(Config{Kind: "fake", Cwd: home, Account: agent.Profile{Kind: "fake", Dir: home}, Carry: []agent.Line{{Role: "user", Text: "previous request"}}})
	if err != nil {
		t.Fatal(err)
	}
	c, err := Dial(cfg.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	defer c.Stop()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		stored, err := ReadConfig(cfg.ID)
		if err == nil && stored.Carry == nil && stored.SessionID == "fake-session" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("carried history never initialized without a new prompt")
}
