package acp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent/event"
)

// TestLiveKimi opens a session with the real `kimi acp` and sends it
// nothing. RUSH_ACP_LIVE=1 runs it.
func TestLiveKimi(t *testing.T) {
	if os.Getenv("RUSH_ACP_LIVE") != "1" {
		t.Skip("RUSH_ACP_LIVE=1 runs it")
	}
	bin, err := exec.LookPath("kimi")
	if err != nil {
		home, _ := os.UserHomeDir()
		bin = filepath.Join(home, ".kimi-code", "bin", "kimi")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Start(ctx, Options{Command: bin, Args: []string{"acp"}, Dir: t.TempDir(), Adapter: "kimi"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.ID() == "" || s.Info().Version == "" || !s.Info().LoadSession {
		t.Fatalf("session %q, info %+v", s.ID(), s.Info())
	}
	init := nextOf[event.Init](t, s)
	if init.SessionID != s.ID() || init.Model == "" || init.Mode == "" {
		t.Fatalf("init: %+v", init)
	}
	t.Logf("kimi %s: session %s, model %s, mode %s, modes %v", init.Version, init.SessionID, init.Model, init.Mode, s.Modes())
}
