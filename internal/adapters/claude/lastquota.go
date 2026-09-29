package claude

import (
	"path/filepath"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/agent/usage"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// LastQuota is the newest reading agtop keeps in usage.json for p's
// folder, or for the login it's signed in as, since agtop reads them per
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
