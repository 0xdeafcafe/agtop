package host

import "github.com/0xdeafcafe/rush/internal/headless"

// Decoder reads one host's lines, in order, as Decode does, except that
// Claude Code's own lines come out as rush's own events: a host from
// before rush's own events, or a client that didn't say hello, has
// Claude's lines, and this reads them as any agent's. One Decoder follows
// one connection.
type Decoder struct{ n headless.Neutral }

// Decode is what line says: none for the traffic only the host needs,
// and more than one where Claude Code says two things at once.
func (d *Decoder) Decode(line []byte) ([]any, error) {
	ev, err := Decode(line)
	if err != nil {
		return nil, err
	}
	h, ok := ev.(headless.Event)
	if !ok {
		return []any{ev}, nil
	}
	evs := d.n.Event(h)
	out := make([]any, len(evs))
	for i, e := range evs {
		out[i] = e
	}
	return out, nil
}
