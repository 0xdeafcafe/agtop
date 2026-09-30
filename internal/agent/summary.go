package agent

import (
	"context"
	"fmt"
)

// Summarizer writes a summary with one of its models: how a session is
// compacted by a model other than its own, a cheaper or a local one.
type Summarizer interface {
	// SummaryModels are the models it can summarise with, best first.
	SummaryModels(ctx context.Context) ([]string, error)
	Summarize(ctx context.Context, model, system, text string) (string, error)
}

// FitTokens is text cut to about n tokens: its start and, most of it, its
// end, where the work in hand is.
func FitTokens(text string, n int) string {
	const perToken = 3 // under-counting chars per token keeps it inside
	if n <= 0 || len(text) <= n*perToken {
		return text
	}
	head, tail := n*perToken/5, n*perToken*4/5
	return text[:head] + fmt.Sprintf("\n\n[… %d characters of the middle left out …]\n\n", len(text)-head-tail) + text[len(text)-tail:]
}
