package host

import (
	"fmt"
	"github.com/0xdeafcafe/rush/internal/agent/event"
	"os"
	"time"
)

func queueExchanges(i *Info) []*event.Exchange {
	q := make([]*event.Exchange, len(i.Queue))
	copy(q, i.QueueExchanges)
	return q
}
func trimExchanges(q []*event.Exchange) []*event.Exchange {
	for _, e := range q {
		if e != nil {
			return q
		}
	}
	return nil
}
func (s *server) recordEvent(ev event.Event) {
	if e, ok := ev.(event.Exchange); ok {
		if e.At.IsZero() {
			e.At = time.Now()
		}
		if err := appendExchange(s.cfg.ID, e); err != nil {
			fmt.Fprintln(os.Stderr, "rush: could not preserve agent exchange:", err)
		}
		ev = e
	}
	if b, err := eventLine(ev); err == nil {
		s.record(b)
	}
}
