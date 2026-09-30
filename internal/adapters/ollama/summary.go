package ollama

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/0xdeafcafe/rush/internal/agent"
)

var _ agent.Summarizer = Adapter{}

// SummaryModels are the server's models.
func (Adapter) SummaryModels(ctx context.Context) ([]string, error) { return names(ctx) }

// Summarize asks the server's model straight, the transcript cut to what
// it was trained to take, less room for the summary.
func (Adapter) Summarize(ctx context.Context, model, system, text string) (string, error) {
	m, err := show(ctx, model)
	if err != nil {
		return "", err
	}
	window := max(window(m), 8192)
	req := map[string]any{
		"model":  model,
		"stream": false,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": agent.FitTokens(text, window-4096)},
		},
		"options": map[string]any{"num_ctx": window},
	}
	var resp struct {
		Message struct{ Content string }
	}
	if err := call(ctx, http.MethodPost, "/api/chat", req, &resp); err != nil {
		return "", err
	}
	out := strings.TrimSpace(resp.Message.Content)
	// A thinking model's reasoning, when the server leaves it in.
	if _, after, ok := strings.Cut(out, "</think>"); ok {
		out = strings.TrimSpace(after)
	}
	if out == "" {
		return "", errors.New(model + " wrote nothing")
	}
	return out, nil
}
