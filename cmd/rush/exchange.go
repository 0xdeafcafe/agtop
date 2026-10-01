package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/host"
)

func exchangePeer(id string) event.Peer {
	p := event.Peer{SessionID: id}
	if c, err := host.ReadConfig(id); err == nil {
		p.Kind = string(agent.Migrated(c.Kind))
		p.Name = c.Name
	}
	return p
}
func outgoingExchange(to, phase, text string, images []string) *event.Exchange {
	from := os.Getenv("RUSH_SESSION")
	if from == "" || from == to {
		return nil
	}
	// A stale/unverifiable environment value is not attributed to an agent.
	if _, err := host.ReadConfig(from); err != nil {
		return nil
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil
	}
	return &event.Exchange{ID: hex.EncodeToString(b), Direction: "received", Phase: phase, Text: text, Images: images, Sender: exchangePeer(from), Receiver: exchangePeer(to)}
}
func recordExchangeAt(id string, e event.Exchange) error {
	if id == "" {
		return nil
	}
	c, err := host.Dial(id)
	if err != nil {
		return err
	}
	defer c.Close()
	proto := 0
	if err := awaitLine(c, 5*time.Second, func(ev any) bool {
		i, ok := ev.(host.InfoEvent)
		if ok {
			proto = i.Info.Proto
		}
		return ok
	}); err != nil {
		return err
	}
	if proto < 8 {
		return fmt.Errorf("session %s runs an older host; restart it to show agent exchanges", id)
	}
	if err := c.RecordExchange(e); err != nil {
		return err
	}
	return awaitLine(c, 5*time.Second, func(ev any) bool {
		x, ok := ev.(event.Exchange)
		return ok && x.ID == e.ID && x.Direction == e.Direction
	})
}
func mirrorOutgoing(e *event.Exchange) error {
	if e == nil {
		return nil
	}
	copy := *e
	copy.Direction = "sent"
	return recordExchangeAt(copy.Sender.SessionID, copy)
}

// exchangePrinter preserves the provider's normal output while collecting the
// final answer in neutral form for the matching return rows on both sessions.
type exchangePrinter struct {
	printer
	decoder host.Decoder
	text    string
}

func (p *exchangePrinter) line(b []byte, ev any) {
	p.printer.line(b, ev)
	events, _ := p.decoder.Decode(b)
	for _, ev := range events {
		switch e := ev.(type) {
		case event.Message:
			if e.Role != "assistant" || e.Parent != "" {
				continue
			}
			var texts []string
			for _, part := range e.Parts {
				if part.Kind == event.Text {
					texts = append(texts, part.Text)
				}
			}
			if len(texts) > 0 {
				p.text = strings.Join(texts, "\n")
			}
		case event.TurnEnd:
			if e.Text != "" {
				p.text = e.Text
			}
		}
	}
}
func recordReturn(request *event.Exchange, child string, text string, code int) {
	if request == nil {
		return
	}
	result := event.Exchange{ID: request.ID + "-result", Phase: "result", Text: text, Sender: exchangePeer(child), Receiver: request.Sender}
	if result.Text == "" {
		result.Text = fmt.Sprintf("Agent finished with exit code %d and no text response.", code)
	}
	result.Direction = "sent"
	if err := recordExchangeAt(child, result); err != nil {
		fmt.Fprintln(os.Stderr, "rush: couldn't record outgoing agent result:", err)
	}
	result.Direction = "received"
	if err := recordExchangeAt(result.Receiver.SessionID, result); err != nil {
		fmt.Fprintln(os.Stderr, "rush: couldn't record returned agent result:", err)
	}
}
