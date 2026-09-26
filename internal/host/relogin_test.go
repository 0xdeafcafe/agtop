package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/headless"
)

// signAs writes the config folder's state file as signed in to email, as
// a switch does: the file every Claude Code started there reads.
func signAs(t *testing.T, acct claude.Account, email string) {
	t.Helper()
	if err := os.MkdirAll(acct.ConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	prof := `{"oauthAccount":{"accountUuid":"uuid-` + email + `","emailAddress":"` + email + `","organizationName":"org of ` + email + `"}}`
	if err := os.WriteFile(acct.StatePath(), []byte(prof), 0o600); err != nil {
		t.Fatal(err)
	}
}

func reloginHost(t *testing.T, prompt string) (*Client, claude.Account, string) {
	t.Helper()
	bin := setup(t)
	acct := claude.Account{Name: "work", ConfigDir: filepath.Join(filepath.Dir(bin), "cfg")}
	signAs(t, acct, "a@example.com")
	cfg, err := Spawn(Config{Cwd: filepath.Dir(bin), Account: acct, Prompt: prompt, Binary: bin, IdleStop: Duration(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	c, err := Dial(cfg.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Stop(); c.Close() })
	return c, acct, filepath.Join(filepath.Dir(bin), "args.log")
}

func startedAs(email string) func(any) bool {
	return func(ev any) bool {
		i, ok := ev.(InfoEvent)
		return ok && i.Info.ClaudePID != 0 && i.Info.StartedAccount != nil && i.Info.StartedAccount.Email == email
	}
}

func asked(t *testing.T, c *Client) headless.PermissionRequest {
	t.Helper()
	return next(t, c, func(ev any) bool { _, ok := ev.(headless.PermissionRequest); return ok }).(headless.PermissionRequest)
}

// finishTurn answers the fake's permission request, which ends its turn.
func finishTurn(t *testing.T, c *Client) {
	t.Helper()
	allow(t, c, asked(t, c))
}

func allow(t *testing.T, c *Client, ask headless.PermissionRequest) {
	t.Helper()
	if err := c.Allow(ask.ID, nil, false); err != nil {
		t.Fatal(err)
	}
}

func launches(t *testing.T, log string) []string {
	t.Helper()
	b, _ := os.ReadFile(log)
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func TestReloginRestartsAnIdleClaude(t *testing.T) {
	c, acct, log := reloginHost(t, "say hi")
	next(t, c, startedAs("a@example.com"))
	finishTurn(t, c)
	next(t, c, inState("idle"))

	signAs(t, acct, "b@example.com")
	if err := c.Relogin(); err != nil {
		t.Fatal(err)
	}
	next(t, c, func(ev any) bool {
		i, ok := ev.(InfoEvent)
		return ok && i.Info.ClaudePID == 0 && i.Info.StartedAccount == nil
	})
	if err := c.Send("again"); err != nil {
		t.Fatal(err)
	}
	next(t, c, startedAs("b@example.com"))
	finishTurn(t, c)
	next(t, c, inState("idle"))
	l := launches(t, log)
	if len(l) != 2 || !strings.Contains(l[1], "--resume") {
		t.Fatalf("want a second launch resuming the conversation, got %q", l)
	}
}

func TestReloginWaitsForTheTurn(t *testing.T) {
	c, acct, log := reloginHost(t, "say hi")
	ask := asked(t, c)
	info := next(t, c, inState("blocked")).(InfoEvent).Info
	if info.StartedAccount == nil || info.StartedAccount.Email != "a@example.com" || info.StartedAccount.Org != "org of a@example.com" {
		t.Fatalf("started account: %+v", info.StartedAccount)
	}
	pid := info.ClaudePID

	signAs(t, acct, "b@example.com")
	if err := c.Relogin(); err != nil {
		t.Fatal(err)
	}
	pending := next(t, c, func(ev any) bool { i, ok := ev.(InfoEvent); return ok && i.Info.RestartAfterTurn }).(InfoEvent).Info
	if pending.ClaudePID != pid || pending.State != "blocked" || pending.StartedAccount.Email != "a@example.com" {
		t.Fatalf("the turn was cut off: %+v", pending)
	}
	// A message sent during the turn waits for it, and goes to the fresh one.
	if err := c.Send("next one"); err != nil {
		t.Fatal(err)
	}
	next(t, c, func(ev any) bool { i, ok := ev.(InfoEvent); return ok && len(i.Info.Queue) == 1 })
	allow(t, c, ask)
	next(t, c, func(ev any) bool { r, ok := ev.(headless.Result); return ok && r.Text == "Said hi." })
	fresh := next(t, c, startedAs("b@example.com")).(InfoEvent).Info
	if fresh.ClaudePID == pid || fresh.RestartAfterTurn || len(fresh.Queue) != 0 {
		t.Fatalf("after the turn: %+v", fresh)
	}
	if alive(pid) {
		t.Errorf("the old Claude Code (%d) is still running", pid)
	}
	finishTurn(t, c)
	next(t, c, inState("idle"))
	l := launches(t, log)
	if len(l) != 2 || !strings.Contains(l[1], "--resume") {
		t.Fatalf("want a second launch resuming the conversation, got %q", l)
	}
}
