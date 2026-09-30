package host

import (
	"strings"
	"testing"
	"time"
)

// A message queued in a turn that runs on and on goes into it once it has
// waited queueLate, rather than waiting for an end that may not come.
func TestLateQueueGoesMidTurn(t *testing.T) {
	setup(t)
	was := queueLate
	queueLate = 50 * time.Millisecond
	defer func() { queueLate = was }()
	ic := &inputConn{}
	s := &server{cfg: Config{ID: "lq"}, conn: ic, clients: map[*conn]struct{}{}}
	s.info.State = "working"
	if err := s.send("take this", nil, false); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	s.mu.Lock()
	early := len(ic.got)
	s.mu.Unlock()
	time.Sleep(100 * time.Millisecond)
	s.mu.Lock()
	defer s.mu.Unlock()
	if early != 0 || len(ic.got) != 1 || !strings.Contains(ic.got[0].Text, "take this") || len(s.info.Queue) != 0 {
		t.Fatalf("sent early %d; then %+v, queue %q", early, ic.got, s.info.Queue)
	}
}
