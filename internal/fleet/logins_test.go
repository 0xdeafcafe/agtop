package fleet

import (
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/state"
)

func loginAt(id string, current bool, fiveHour, sevenDay float64) LoginView {
	return LoginView{
		Login:   claude.Login{ID: id, Name: id},
		Current: current,
		Usage: claude.Usage{
			FetchedAt: time.Now(),
			FiveHour:  claude.Window{Present: true, Percent: fiveHour},
			SevenDay:  claude.Window{Present: true, Percent: sevenDay},
		},
	}
}

func TestNextLogin(t *testing.T) {
	for _, tc := range []struct {
		name    string
		logins  []LoginView
		stopped bool
		want    string // "" for no switch
	}{
		{"room left", []LoginView{loginAt("a", true, 60, 40), loginAt("b", false, 0, 0)}, false, ""},
		{"5h nearly out", []LoginView{loginAt("a", true, 96, 40), loginAt("b", false, 10, 20)}, false, "b"},
		{"7d nearly out", []LoginView{loginAt("a", true, 10, 97), loginAt("b", false, 10, 20)}, false, "b"},
		{"most room wins", []LoginView{loginAt("a", true, 99, 40), loginAt("b", false, 50, 20), loginAt("c", false, 5, 30)}, false, "c"},
		{"others nearly out too", []LoginView{loginAt("a", true, 96, 40), loginAt("b", false, 95, 20)}, false, ""},
		{"stopped switches below the threshold", []LoginView{loginAt("a", true, 80, 40), loginAt("b", false, 90, 20)}, true, "b"},
		{"only one", []LoginView{loginAt("a", true, 99, 99)}, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := NextLogin(tc.logins, tc.stopped)
			if tc.want == "" {
				if ok {
					t.Fatalf("switched to %s, want no switch", got.ID)
				}
				return
			}
			if !ok || got.ID != tc.want {
				t.Fatalf("got %q (%v), want %q", got.ID, ok, tc.want)
			}
		})
	}
}

func TestNextLoginSkipsLoginsWithoutReading(t *testing.T) {
	unread := LoginView{Login: claude.Login{ID: "b"}}
	stale := loginAt("c", false, 0, 0)
	stale.Usage.FetchedAt = time.Now().Add(-time.Hour)
	got, ok := NextLogin([]LoginView{loginAt("a", true, 100, 55), unread, stale, loginAt("d", false, 1, 0)}, true)
	if !ok || got.ID != "d" {
		t.Fatalf("got %q (%v), want d, the only one with a reading", got.ID, ok)
	}
}

func TestNextLoginIgnoresStaleReading(t *testing.T) {
	cur := loginAt("a", true, 99, 40)
	cur.Usage.FetchedAt = time.Now().Add(-time.Hour)
	if _, ok := NextLogin([]LoginView{cur, loginAt("b", false, 0, 0)}, false); ok {
		t.Fatal("switched on an hour-old reading")
	}
}

// A reading made before ~/.claude was signed in as another account is that
// account's: it mustn't be shown, or switched on, as the new one's.
func TestFreshestSkipsOtherAccountsReading(t *testing.T) {
	l := NewLoader(&state.Store{})
	acct := claude.Account{Name: "default", ConfigDir: "/x"}
	now := time.Now()
	l.SetFetched(acct.ConfigDir, claude.Usage{AccountID: "old", FetchedAt: now, FiveHour: claude.Window{Present: true, Percent: 97}})
	cached := claude.Usage{AccountID: "new", FetchedAt: now.Add(-time.Minute), FiveHour: claude.Window{Present: true, Percent: 3}}
	if u := l.freshest(acct, cached); u.AccountID != "new" || u.FiveHour.Percent != 3 {
		t.Fatalf("got %s at %.0f%%, want new at 3%%", u.AccountID, u.FiveHour.Percent)
	}
	l.SetFetched(acct.ConfigDir, claude.Usage{AccountID: "new", FetchedAt: now, FiveHour: claude.Window{Present: true, Percent: 5}})
	if u := l.freshest(acct, cached); u.FiveHour.Percent != 5 {
		t.Fatalf("got %.0f%%, want the newer reading's 5%%", u.FiveHour.Percent)
	}
}

func TestLoginsUseOwnReadingAfterSwitch(t *testing.T) {
	l := NewLoader(&state.Store{})
	now := time.Now()
	cfg := state.Config{Logins: []claude.Login{{ID: "a", Name: "a"}, {ID: "b", Name: "b"}}}
	l.SetFetched("login:a", claude.Usage{AccountID: "a", FetchedAt: now.Add(-time.Minute), FiveHour: claude.Window{Present: true, Percent: 97}})
	l.SetFetched("login:b", claude.Usage{AccountID: "b", FetchedAt: now.Add(-time.Minute), FiveHour: claude.Window{Present: true, Percent: 4}})
	// ~/.claude is now signed in as b, with a fresher reading of its own.
	root := AccountView{Usage: claude.Usage{AccountID: "b", FetchedAt: now, FiveHour: claude.Window{Present: true, Percent: 6}}}
	got := l.logins(cfg, root, now)
	if !got[1].Current || got[1].Usage.FiveHour.Percent != 6 {
		t.Fatalf("b: current %v at %.0f%%, want current at 6%%", got[1].Current, got[1].Usage.FiveHour.Percent)
	}
	if got[0].Current || got[0].Usage.FiveHour.Percent != 97 {
		t.Fatalf("a: current %v at %.0f%%, want its own 97%%", got[0].Current, got[0].Usage.FiveHour.Percent)
	}
}
