package host

import (
	"errors"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/event"
)

// Decoder reads one host's lines, in order, as Decode does, except that
// the native agent's own lines come out as rush's own events: a host from
// before rush's own events, or a client that didn't say hello, has
// Claude Code's lines, and this reads them as any agent's. One Decoder
// follows one connection.
type Decoder struct{ n agent.Neutral }

// Decode is what line says: none for the traffic only the host needs,
// and more than one where Claude Code says two things at once.
func (d *Decoder) Decode(line []byte) ([]any, error) {
	ev, err := Decode(line)
	if err != nil {
		return nil, err
	}
	evs, ok := neutral(&d.n).Event(ev)
	if !ok {
		return []any{ev}, nil
	}
	out := make([]any, len(evs))
	for i, e := range evs {
		out[i] = e
	}
	return out, nil
}

// errNoNative is a line of an agent's own when no agent reads them.
var errNoNative = errors.New("host: no agent reads this line")

// nativeLine is a line the native agent wrote (agent.Native) as its own
// event.
func nativeLine(line []byte) (any, error) {
	n, ok := agent.NativeAgent()
	if !ok {
		return nil, errNoNative
	}
	return n.Line(line)
}

// neutral is *n, started on first use: the native agent's reader of one
// session's own events as rush's. With no native agent it reads none.
func neutral(n *agent.Neutral) agent.Neutral {
	if *n == nil {
		if na, ok := agent.NativeAgent(); ok {
			*n = na.Neutral()
		} else {
			*n = noNeutral{}
		}
	}
	return *n
}

type noNeutral struct{}

func (noNeutral) Event(any) ([]event.Event, bool) { return nil, false }
