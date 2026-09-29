package state

import "github.com/0xdeafcafe/rush/internal/agent"

// OwnAgent says new sessions run the logins agent's own agent, not one of
// the definitions it can start as.
func (d Dispatch) OwnAgent() bool {
	if d.Agent == "" {
		return true
	}
	n, ok := agent.As[agent.BuiltinDefNamer](agent.Kind(LoginsKind))
	return ok && d.Agent == n.BuiltinDef()
}
