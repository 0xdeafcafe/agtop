package fleet

import (
	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/state"
)

// Target is one agent's transcript as the scanner needs it.
type Target = agent.SpendTarget

// Scanner prices agents' transcripts. Every rush session writes the
// logins agent's, so it's that agent's scanner that reads them.
type Scanner struct{ s agent.SpendScanner }

// NewScanner reads nothing: its cost cache is read as it first runs, off
// the UI goroutine.
func NewScanner() *Scanner {
	if r, ok := agent.As[agent.SpendReader](agent.Kind(state.LoginsKind)); ok {
		return &Scanner{r.SpendScanner()}
	}
	return &Scanner{}
}

// Run scans every target whose files grew and returns the new totals.
func (s *Scanner) Run(targets []Target) map[string]Spend {
	if s.s == nil {
		return map[string]Spend{}
	}
	return s.s.Run(targets)
}

// Flush saves the cost cache unless a scan is mid-way; the cache also saves
// itself every 30s, so skipping one save loses nothing.
func (s *Scanner) Flush() {
	if s.s != nil {
		s.s.Flush()
	}
}
