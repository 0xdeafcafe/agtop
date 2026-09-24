package statusline

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/0xdeafcafe/agtop/internal/state"
)

// Bars are agtop's own status lines, built the same way as Claude Code's
// but drawn by agtop from what it knows: Top at the top right of the
// window, about every agent at once, and Agent at the top of an agent's
// Session. Their segments are agtop's (see ui/bars.go).
type Bars struct {
	Top   Layout `json:"top"`
	Agent Layout `json:"agent"`
}

// BarLines is how many lines each of agtop's own has room for.
const BarLines = 2

// DefaultTop and DefaultAgent are what agtop showed before they could be
// changed.
func DefaultTop() Layout {
	return Layout{Lines: [][]string{{"today", "usage"}, {"ram", "cpu", "tmp"}}, Sep: " · "}
}

func DefaultAgent() Layout {
	return Layout{Lines: [][]string{{"context", "cost"}, {"folder", "branch", "model", "effort", "mode", "tmp"}}, Sep: " · "}
}

// BarsPath is where they're kept.
func BarsPath() string { return filepath.Join(state.Dir(), "bars.json") }

// LoadBars reads them; one never saved is its default.
func LoadBars() Bars {
	b := Bars{Top: DefaultTop(), Agent: DefaultAgent()}
	raw, err := os.ReadFile(BarsPath())
	if err != nil {
		return b
	}
	var got Bars
	if json.Unmarshal(raw, &got) != nil {
		return b
	}
	if got.Top.Lines != nil {
		b.Top = got.Top
	}
	if got.Agent.Lines != nil {
		b.Agent = got.Agent
	}
	return b
}

// SaveBars writes them.
func SaveBars(b Bars) error {
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(BarsPath()), 0o700); err != nil {
		return err
	}
	return os.WriteFile(BarsPath(), append(raw, '\n'), 0o600)
}
