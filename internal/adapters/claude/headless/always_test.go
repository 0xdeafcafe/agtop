package headless

import (
	"encoding/json/jsontext"
	"testing"
)

// Always allowing says what it saves, as Claude Code's suggestion names it.
func TestAlwaysLabel(t *testing.T) {
	got := alwaysLabel(jsontext.Value(`[{"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"pnpm lint:*"}],"behavior":"allow","destination":"localSettings"}]`))
	if got != "Always allow Bash(pnpm lint:*)" {
		t.Fatalf("got %q", got)
	}
	if got := alwaysLabel(jsontext.Value(`[{"type":"setMode","mode":"acceptEdits"}]`)); got != "Always allow" {
		t.Fatalf("no rule: %q", got)
	}
}
