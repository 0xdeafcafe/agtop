package state

import "github.com/0xdeafcafe/rush/internal/claude"

// OwnAgent says new sessions run the logins agent's own agent, not one of
// the definitions it can start as.
func (d Dispatch) OwnAgent() bool { return d.Agent == "" || d.Agent == claude.DefaultAgent }
