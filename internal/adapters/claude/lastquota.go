package claude

import (
	"path/filepath"
	"time"

	"github.com/0xdeafcafe/rush/internal/adapters/claude/claude"
	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/usage"
	"github.com/0xdeafcafe/rush/internal/state"
)

// LastQuota is the newest reading rush keeps in usage.json for p's
// folder, or for the login it's signed in as, since rush reads them per
// login.
func (Adapter) LastQuota(p agent.Profile) (usage.Quota, bool) {
	all := claude.LoadFetchedUsage(filepath.Join(state.Dir(), "usage.json"))
	f, ok := all[p.Dir]
	if id := claude.SignedInAs(claude.Account{ConfigDir: p.Dir}); id != "" {
		if g, found := all[claude.Login{ID: id}.UsageKey()]; found && g.Usage.FetchedAt.After(f.Usage.FetchedAt) {
			f, ok = g, true
		}
	}
	return f.Usage.Since(time.Now()).Quota(""), ok
}
