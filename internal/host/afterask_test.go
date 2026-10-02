package host

import (
	"strings"
	"testing"
)

// A message sent now while the agent asks something waits behind the
// question, so it can't call it off, and goes once it's answered.
func TestSendNowWaitsForQuestion(t *testing.T) {
	setup(t)
	ic := &inputConn{}
	s := &server{cfg: Config{ID: "aa", Kind: "claude"}, conn: ic, clients: map[*conn]struct{}{}, pending: map[string]asked{"q1": {question: true}}}
	s.info.State = "blocked"
	if err := s.send("also do this", nil, true); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	if len(ic.got) != 0 || len(s.info.Queue) != 1 {
		s.mu.Unlock()
		t.Fatalf("went to the agent over its question: sent %+v, queue %q", ic.got, s.info.Queue)
	}
	s.answered("q1")
	s.afterAnswer()
	s.mu.Unlock()
	if len(ic.got) != 1 || !strings.Contains(ic.got[0].Text, "also do this") || len(s.info.Queue) != 0 {
		t.Fatalf("not sent once answered: %+v, queue %q", ic.got, s.info.Queue)
	}
}
