package state

import (
	"testing"

	"github.com/0xdeafcafe/rush/internal/claude"
)

// A copy has the config as it is now, shares none of it with the store,
// and while the config is unchanged copies share one decoded config.
func TestStoreCopyFollowsChanges(t *testing.T) {
	s := &Store{}
	s.Config.GroupBy = "status"
	s.Config.Logins = []claude.Login{{ID: "a", Name: "a"}}
	one, two := s.Copy(), s.Copy()
	if one.Config.GroupBy != "status" || len(two.Config.Logins) != 1 {
		t.Fatalf("copies: %+v %+v", one.Config, two.Config)
	}
	if &one.Config.Logins[0] != &two.Config.Logins[0] {
		t.Error("an unchanged config was decoded again")
	}
	if &one.Config.Logins[0] == &s.Config.Logins[0] {
		t.Fatal("a copy shares the store's logins")
	}
	s.Config.GroupBy = "agent"
	s.Config.Logins[0].Name = "renamed"
	three := s.Copy()
	if three.Config.GroupBy != "agent" || three.Config.Logins[0].Name != "renamed" {
		t.Fatalf("after a change: %+v", three.Config)
	}
	if one.Config.GroupBy != "status" || one.Config.Logins[0].Name != "a" {
		t.Fatal("a change reached an earlier copy")
	}
}
