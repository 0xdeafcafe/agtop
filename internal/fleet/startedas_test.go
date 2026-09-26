package fleet

import (
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/claude"
)

func TestStartedAsFlagsTheOldAccount(t *testing.T) {
	a := claude.Who{ID: "a", Email: "a@example.com"}
	b := claude.Who{ID: "b", Email: "b@example.com"}
	root := claude.Account{Name: "default", ConfigDir: "/home/.claude"}
	t0 := time.Now().Add(-time.Hour)
	sw := []claude.Switch{
		{At: t0.Add(10 * time.Minute), Dir: root.ConfigDir, From: a, To: b},
		{At: t0.Add(20 * time.Minute), Dir: "/elsewhere", From: b, To: a},
	}

	// An agtop session the host recorded, mid-turn after the switch.
	hosted := &Agent{Acct: root, Agtop: true, Restarting: true}
	hosted.State = "working"
	hosted.startedAs(&a, time.Time{}, sw, b)
	if !hosted.OldAccount || hosted.AccountNote() != "on a@example.com until its turn ends" {
		t.Fatalf("hosted mid-turn: %v %q", hosted.OldAccount, hosted.AccountNote())
	}
	// Restarted: the host now reports the new account.
	hosted.startedAs(&b, time.Time{}, sw, b)
	if hosted.OldAccount || hosted.AccountNote() != "" {
		t.Fatalf("hosted after restart: %v %q", hosted.OldAccount, hosted.AccountNote())
	}

	// A terminal session started before the switch, known by its start.
	term := &Agent{Acct: root, Interactive: true}
	term.startedAs(nil, t0, sw, b)
	if !term.OldAccount || term.StartedAs != a || term.AccountNote() != "on a@example.com until you restart it" {
		t.Fatalf("terminal before the switch: %+v %q", term.StartedAs, term.AccountNote())
	}
	// One started after it is on the account in use; a switch in another
	// folder doesn't touch it.
	late := &Agent{Acct: root, Interactive: true}
	late.startedAs(nil, t0.Add(15*time.Minute), sw, b)
	if late.OldAccount || late.StartedAs != b {
		t.Fatalf("terminal after the switch: %+v", late.StartedAs)
	}
	// Switched back since: started on a, and a is in use again.
	back := append(sw, claude.Switch{At: t0.Add(30 * time.Minute), Dir: root.ConfigDir, From: b, To: a})
	term.startedAs(nil, t0, back, a)
	if term.OldAccount {
		t.Fatal("a session on the account switched back to is flagged")
	}
}
