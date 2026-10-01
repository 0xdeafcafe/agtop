package host

import (
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
)

func TestCheckedCompactionRejectsChangedConversation(t *testing.T) {
	setup(t)
	before := time.Now()
	for _, scenario := range []string{"new turn", "different branch", "queued input", "working", "missing snapshot"} {
		t.Run(scenario, func(t *testing.T) {
			s := &server{cfg: Config{ID: "guard", Kind: "fake", SessionID: "old", Name: "keep me", Account: agent.Profile{Kind: "fake"}}, info: Info{SessionID: "old", State: "idle", UpdatedAt: before}}
			o := op{Op: "compacted_checked", Text: "new", Message: "stale summary", ExpectedSession: "old", ExpectedUpdatedAt: before}
			switch scenario {
			case "new turn":
				s.info.UpdatedAt = before.Add(time.Second)
			case "different branch":
				s.info.SessionID = "another"
			case "queued input":
				s.info.Queue = []string{"keep this"}
			case "working":
				s.info.State = "working"
			case "missing snapshot":
				o.ExpectedUpdatedAt = time.Time{}
			}
			if err := s.do(o); err == nil {
				t.Fatal("stale compaction accepted")
			}
			if s.cfg.SessionID != "old" || s.cfg.Name != "keep me" || len(s.cfg.Branches) != 0 {
				t.Fatal("rejected compaction mutated conversation")
			}
		})
	}
}

func TestCheckedCompactionRequiresCapableHost(t *testing.T) {
	c := &Client{}
	if err := c.CompactedIfUnchanged("new", "summary", Branch{}, Info{Proto: 8}); err == nil {
		t.Fatal("older host accepted unsafe operation")
	}
}
