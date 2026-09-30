package host

import (
	"slices"
	"testing"

	"github.com/0xdeafcafe/rush/internal/agent"
)

// sentConn keeps what it was sent, and the turns it was asked to stop.
type sentConn struct {
	interruptConn
	sent []string
}

func (c *sentConn) Send(in agent.Input) error { c.sent = append(c.sent, in.Text); return nil }

// Guided mid-turn, a message goes into the turn under way: nothing is
// stopped and nothing queued. An agent that can't take one queues it.
func TestGuideGoesIntoTheTurn(t *testing.T) {
	setup(t)
	c := &sentConn{}
	s := &server{cfg: Config{ID: "g", Kind: "claude"}, conn: c, clients: map[*conn]struct{}{}}
	s.info.State = "working"
	if err := s.do(op{Op: "send", Text: "also the auth path", Guide: true}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(c.sent, "also the auth path") || c.stops != 0 || len(s.info.Queue) != 0 {
		t.Fatalf("sent %q stops %d queue %q", c.sent, c.stops, s.info.Queue)
	}
	o := &sentConn{}
	s = &server{cfg: Config{ID: "g2", Kind: "nothing-guides"}, conn: o, clients: map[*conn]struct{}{}}
	s.info.State = "working"
	if err := s.do(op{Op: "send", Text: "later", Guide: true}); err != nil {
		t.Fatal(err)
	}
	if len(o.sent) != 0 || !slices.Equal(s.info.Queue, []string{"later"}) {
		t.Fatalf("an agent that can't be guided should queue it: sent %q queue %q", o.sent, s.info.Queue)
	}
}
