package event

import "time"

// Peer is the known identity of a session participating in an exchange. Empty
// fields mean unknown; Kind identifies the configured agent/provider choice.
type Peer struct {
	SessionID string
	Kind      string
	Name      string
}

// Exchange records communication between agents independently of user prompts.
// Both transcripts retain the same ID. Direction is sent or received; Phase is
// message, request, or result. Text and attachment references are never shortened.
type Exchange struct {
	At        time.Time
	Archived  bool // restored history; never starts a live turn
	ID        string
	Direction string
	Phase     string
	Text      string
	Images    []string
	Sender    Peer
	Receiver  Peer
}

func (Exchange) event() {}
