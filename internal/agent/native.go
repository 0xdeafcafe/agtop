package agent

import (
	"encoding/json/jsontext"

	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/agent/tool"
)

// Native is the agent whose own words rush still reads apart from its
// Driver: hosts an older rush started send its lines as it wrote them,
// its transcripts keep them, and a conversation's steps keep every call
// in its tools' names and inputs (convo/words.go). The host and convo read
// them through it, never knowing their shape. One adapter at most is.
type Native interface {
	// Line reads one line the agent wrote as its own event.
	Line(line []byte) (any, error)
	// Message reads a message a transcript keeps (its type, the message
	// and the result a tool gave) as the agent's own event.
	Message(typ string, message, result jsontext.Value) (any, error)
	// Neutral starts reading one session's own events as rush's.
	Neutral() Neutral
	// KindOf is what the agent's tool of this name does.
	KindOf(name string) tool.Kind
	// Call is a call of the agent's tool, in its words, as rush's own.
	Call(id, name string, input jsontext.Value) tool.Call
	// Output is how call c came out, from the text and the result the
	// agent gave back in its words.
	Output(c tool.Call, text string, isError bool, result jsontext.Value) tool.Output
}

// Neutral reads one session's own events, as its Native read them, as
// rush's: none for what only the host needs, more than one where the
// agent says two things at once. ok is false for a value that isn't one
// of the agent's own events.
type Neutral interface {
	Event(ev any) (evs []event.Event, ok bool)
}

// native is the registered Native adapter, if any; set by Register.
var native Native

// NativeAgent is the registered Native adapter; false when none is.
func NativeAgent() (Native, bool) {
	mu.RLock()
	defer mu.RUnlock()
	return native, native != nil
}
