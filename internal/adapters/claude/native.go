package claude

import (
	"encoding/json/jsontext"

	"github.com/0xdeafcafe/rush/internal/adapters/claude/claude"
	"github.com/0xdeafcafe/rush/internal/adapters/claude/headless"
	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/agent/tool"
)

// Claude Code is rush's Native agent: its stream-json lines, its
// transcripts' messages and its tools' words.

func (Adapter) Line(line []byte) (any, error) { return headless.Decode(line) }

func (Adapter) Message(typ string, message, result jsontext.Value) (any, error) {
	return headless.DecodeMessage(typ, message, result)
}

func (Adapter) Neutral() agent.Neutral { return &neutral{} }

func (Adapter) KindOf(name string) tool.Kind { return claude.KindOf(name) }

func (Adapter) Call(id, name string, input jsontext.Value) tool.Call {
	return claude.Call(id, name, input)
}

func (Adapter) Output(c tool.Call, text string, isError bool, result jsontext.Value) tool.Output { //nolint:gocritic // agent.Native's signature, a Call as convo keeps it
	return claude.Output(c, text, isError, result)
}

// neutral reads one session's headless events as rush's.
type neutral struct{ n headless.Neutral }

func (n *neutral) Event(ev any) ([]event.Event, bool) {
	h, ok := ev.(headless.Event)
	if !ok {
		return nil, false
	}
	return n.n.Event(h), true
}

var _ agent.Native = Adapter{}
