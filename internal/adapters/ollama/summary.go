package ollama

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/0xdeafcafe/rush/internal/agent"
)

var _ agent.Summarizer = Adapter{}

// SummaryModels excludes classifiers even when their base architecture
// advertises completion: a decision letter cannot preserve a conversation.
func (Adapter) SummaryModels(ctx context.Context) ([]string, error) {
	models, err := InstalledModels(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, model := range models {
		if why := summaryUnsupported(model); why != "" {
			continue
		}
		out = append(out, model.Name)
	}
	return out, nil
}

// SummaryExclusions explains unavailable local models without preventing
// callers such as automatic titling from using eligible ones.
func (Adapter) SummaryExclusions() []string {
	var out []string
	for _, m := range CachedInstalledModels() {
		if why := summaryUnsupported(m); why != "" {
			out = append(out, m.Name+": "+why)
		}
	}
	return out
}

func summaryUnsupported(m Model) string {
	if m.Can("decision") {
		return "a decision classifier; it selects an option, not a conversation summary"
	}
	if !m.Can("completion") {
		return "not a text-completion model"
	}
	return ""
}

// Summarize uses a bounded context and summarizes every part of long
// input before combining the results. No middle of the conversation is
// silently discarded to fit the local model.
func (Adapter) Summarize(ctx context.Context, model, system, text string) (string, error) {
	if err := EnsureRunning(ctx); err != nil {
		return "", err
	}
	m, err := show(ctx, model)
	if err != nil {
		return "", err
	}
	if why := summaryUnsupported(m); why != "" {
		return "", fmt.Errorf("%s is %s", model, why)
	}
	contextPrefs.Lock()
	readContextsLocked()
	n := contextPrefs.values[model]
	contextPrefs.Unlock()
	if n == 0 {
		n = 32768
	}
	if m.MaxContext > 0 {
		n = min(n, m.MaxContext)
	}
	output := min(4096, n/4)
	// A byte can require its own token. Reserve using bytes rather than
	// a prose-only characters/token estimate that can truncate code or CJK.
	budget := n - output - len(system) - 1024
	if budget < 1024 {
		return "", errors.New("context is too small to summarize safely")
	}
	for len(text) > budget {
		parts := summaryChunks(text, budget)
		var merged strings.Builder
		for i, part := range parts {
			summary, err := summarizePart(ctx, model, system+"\nThis is one ordered part of a larger input. Preserve facts, constraints, and unfinished work from this part for a later combined summary.", part, n, output)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&merged, "\nPart %d of %d:\n%s\n", i+1, len(parts), summary)
		}
		next := merged.String()
		if len(next) >= len(text) {
			return "", errors.New("local model did not reduce the input; conversation left unchanged")
		}
		text = next
	}
	return summarizePart(ctx, model, system, text, n, output)
}

func summaryChunks(text string, most int) []string {
	var out []string
	for len(text) > most {
		cut := most
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		if at := strings.LastIndexByte(text[:cut], '\n'); at > cut/2 {
			cut = at + 1
		}
		out = append(out, text[:cut])
		text = text[cut:]
	}
	if text != "" {
		out = append(out, text)
	}
	return out
}

func summarizePart(ctx context.Context, model, system, text string, window, output int) (string, error) {
	req := map[string]any{
		"model": model, "stream": false, "think": false,
		"messages": []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": text}},
		"options":  map[string]any{"num_ctx": window, "num_predict": output, "temperature": 0},
	}
	var resp struct {
		Message    struct{ Content string }
		DoneReason string `json:"done_reason"`
	}
	if err := call(ctx, http.MethodPost, "/api/chat", req, &resp); err != nil {
		return "", err
	}
	if resp.DoneReason == "length" {
		return "", errors.New("local summary reached its output limit; conversation left unchanged")
	}
	out := strings.TrimSpace(resp.Message.Content)
	if _, after, ok := strings.Cut(out, "</think>"); ok {
		out = strings.TrimSpace(after)
	}
	if out == "" {
		return "", errors.New(model + " wrote nothing")
	}
	return out, nil
}
