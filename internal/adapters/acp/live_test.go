package acp

import (
	"context"
	"github.com/0xdeafcafe/rush/internal/agent"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// This uses the adapter entry point, including the selected profile and model.
// RUSH_KIMI_PROMPT=1 additionally spends one tiny model request to verify streaming,
// persistence and resume. The prompt runs in an empty temporary directory.
func TestLiveKimiAdapter(t *testing.T) {
	if os.Getenv("RUSH_ACP_LIVE") != "1" {
		t.Skip("RUSH_ACP_LIVE=1 runs it")
	}
	a := Kimi{Known[0]}
	cfg, err := readKimiConfig(kimiHome())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	opts := agent.StartOptions{Dir: t.TempDir(), Profile: agent.Profile{Kind: "kimi", Dir: kimiHome()}, Model: cfg.Default}
	conn, err := a.Start(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	s := conn.(*Session)
	defer s.Close()
	init := nextOf[event.Init](t, s)
	if init.SessionID == "" {
		t.Fatal("missing session ID")
	}
	if err := s.SetMode("plan"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMode("default"); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("RUSH_KIMI_PROMPT") == "1" {
		if err := s.Send(agent.Input{Text: "Reply exactly RUSH_KIMI_OK. Do not call any tools."}); err != nil {
			t.Fatal(err)
		}
		answer := ""
		done := false
		for !done {
			select {
			case ev, ok := <-s.Events():
				if !ok {
					t.Fatal("session closed before answering")
				}
				switch e := ev.(type) {
				case event.Message:
					if e.Role == "assistant" {
						for _, p := range e.Parts {
							if p.Kind == event.Text {
								answer += p.Text
							}
						}
					}
				case event.Approval:
					_ = s.Answer(e.ID, "")
					t.Fatal("no-tool smoke prompt unexpectedly requested a tool")
				case event.TurnEnd:
					if e.Err != "" {
						t.Fatal(e.Err)
					}
					done = true
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
		if !strings.Contains(answer, "RUSH_KIMI_OK") {
			t.Fatal("model did not return smoke-test marker")
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	opts.Resume, opts.SessionID = true, s.ID()
	resumed, err := a.Start(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	if resumed.(*Session).ID() != s.ID() {
		t.Fatal("resume opened another conversation")
	}
	if os.Getenv("RUSH_KIMI_PROMPT") == "1" {
		found := false
		for _, past := range a.Past(opts.Profile) {
			if past.ID != s.ID() {
				continue
			}
			evs, err := a.History(past, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			for _, ev := range evs {
				if m, ok := ev.(event.Message); ok && m.Role == "assistant" {
					for _, p := range m.Parts {
						if p.Kind == event.Text && strings.Contains(p.Text, "RUSH_KIMI_OK") {
							found = true
						}
					}
				}
			}
		}
		if !found {
			t.Fatal("saved Kimi reply was not discoverable through the adapter")
		}
	}
	t.Logf("Kimi %s: adapter start, model selection, modes and resume passed", s.Info().Version)
}
