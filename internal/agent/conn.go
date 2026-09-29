package agent

import (
	"context"
	"encoding/json/jsontext"
)

// What a session agtop hosts can do beyond Conn: each is an optional
// interface a Conn has only when its agent can, found with a type
// assertion. The host offers an op only to a session whose Conn has it.

// ToolServer is an MCP server agtop serves in process for a session: its
// own drawing tools, or an approved plugin's.
type ToolServer struct {
	Name string
	// Trusted are its tools that never ask, by their own name: agtop's
	// own only draw.
	Trusted []string
	// Handle answers one JSON-RPC message. It may take a while; the
	// session carries on meanwhile.
	Handle func(msg jsontext.Value) jsontext.Value
}

// Responder is a Conn that takes an approval's answer with more than an
// option: the call's input as you changed it, in the agent's own words,
// and a refusal's message, and whether a refusal stops the turn.
type Responder interface {
	Allow(approvalID string, input jsontext.Value, always bool) error
	Deny(approvalID, message string, interrupt bool) error
}

// Asker is a Conn that passes a request in the agent's own control
// protocol through for a client, and waits for its reply.
type Asker interface {
	Ask(ctx context.Context, req jsontext.Value) (jsontext.Value, error)
}

// ContextReader is a Conn that breaks down what fills the context window,
// as the agent counts it, in the shape clients read it.
type ContextReader interface {
	ContextUsage(ctx context.Context) (jsontext.Value, error)
}

// TaskStopper is a Conn that stops one task beside the turn, leaving the
// turn and the rest running.
type TaskStopper interface {
	StopTask(id string) error
}

// Backgrounder is a Conn that moves a call the turn is waiting on into the
// background, so the turn carries on; an empty id moves every one.
type Backgrounder interface {
	Background(callID string) error
}

// Staler is a Conn whose session holds a sign-in its profile has since
// left: it should rest once its turn ends, and pick up on the new one.
type Staler interface {
	Stale() bool
}

// QuotaKeeper is a Conn that keeps its own quota readings where its agent
// reads them: the host doesn't record its event.Quota again.
type QuotaKeeper interface {
	KeepsQuota()
}

// Ender is a Conn that says why its session ended, once Events closes.
type Ender interface {
	Err() error
}

// PIDer is a Conn whose agent is a process of its own.
type PIDer interface {
	PID() int
}
