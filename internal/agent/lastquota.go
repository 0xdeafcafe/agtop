package agent

import "github.com/0xdeafcafe/agtop/internal/agent/usage"

// LastQuotaReader is an agent whose last reading of a profile's limits
// agtop can read at once, without asking anyone: for a status line drawn
// many times a second.
type LastQuotaReader interface {
	// LastQuota is p's limits as last read, with the windows that have
	// reset since emptied; false if there's no reading.
	LastQuota(p Profile) (usage.Quota, bool)
}
