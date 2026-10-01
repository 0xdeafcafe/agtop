package codex

import (
	"strings"

	"github.com/0xdeafcafe/rush/internal/agent"
)

// ponytail: efforts as models_cache.json's supported_reasoning_levels had
// them (2026-09); read them per model if they drift.
// Choices are Codex's reasoning efforts and rush's three presets of its
// approval policy and sandbox (modes). Its models come and go with the
// account, so Settings offers the ones your sessions have used, or a typed
// one.
func (Adapter) Choices() agent.Choices {
	return agent.Choices{
		Efforts: []agent.Choice{
			{ID: "low", Note: "quick; fine for simple, well-specified tasks."},
			{ID: "medium", Note: "a balance of speed and care."},
			{ID: "high", Note: "careful; for tricky work."},
			{ID: "xhigh", Note: "more careful still; for hard problems."},
			{ID: "max", Note: "as much as the model has, bar ultra."},
			{ID: "ultra", Note: "the most, on the models that have it (not luna)."},
		},
		Modes: []agent.Choice{
			{ID: "read-only", Note: "reads and answers; asks before changing anything."},
			{ID: "auto", Note: "edits in the workspace without asking; asks before the network or outside it."},
			{ID: "full-access", Note: "never asks, and can reach anything. Only for sandboxed or throwaway work."},
		},
	}
}

// Reads is what model reads, by family: every GPT reads images, but
// Spark, which reads only text. Codex sends nothing else.
// ponytail: a table by name; app-server's model/list says each model's
// input modalities, for when a new family gets it wrong.
func (Adapter) Reads(model string) (agent.Media, bool) {
	switch {
	case strings.Contains(model, "spark"):
		return 0, true
	case strings.HasPrefix(model, "gpt-"):
		return agent.MediaImage, true
	}
	return 0, false
}
