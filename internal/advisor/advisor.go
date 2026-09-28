// Package advisor watches how you use your agents and says what would cut
// tokens or time. It works in two passes: a cheap model (Haiku) reads a
// digest agtop works out itself and proposes candidates, and now and then
// an expensive one (Opus) checks the ones worth money against the
// transcripts before they reach you. It's opt-in: nothing runs until you
// turn it on.
package advisor

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/0xdeafcafe/agtop/internal/state"
)

const (
	// Gap is the least time between passes.
	Gap = 3 * time.Hour
	// MinRequests is how many new requests make a pass worth running.
	MinRequests = 150
	// ReviewAt is the dollars a week a candidate must be worth for Opus to
	// check it.
	ReviewAt = 1.0
	// ReviewsPerDay caps the Opus reviews in any 24 hours.
	ReviewsPerDay = 3
	// Keep is how many findings are kept.
	Keep = 12
)

// What a Finding's Status can be.
const (
	Candidate = "candidate" // Haiku's, not checked
	Confirmed = "confirmed" // Opus checked it and it holds
	Rejected  = "rejected"  // Opus checked it and it doesn't; kept so it isn't proposed again
)

// Finding is something the advisor noticed.
type Finding struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Detail   string    `json:"detail"`
	Evidence []string  `json:"evidence,omitempty"`
	Weekly   float64   `json:"weekly,omitempty"` // dollars a week at stake, roughly
	Fix      string    `json:"fix,omitempty"`    // a saver's ID
	Open     string    `json:"open,omitempty"`   // a file to change
	Status   string    `json:"status"`
	Note     string    `json:"note,omitempty"` // the reviewer's
	At       time.Time `json:"at"`
}

// Record is what the advisor keeps between runs.
type Record struct {
	LastRun  time.Time   `json:"lastRun,omitzero"`
	Runs     int         `json:"runs,omitempty"`
	Reviews  []time.Time `json:"reviews,omitempty"`
	Spent    float64     `json:"spent,omitempty"` // what the advisor itself has cost
	Err      string      `json:"err,omitempty"`   // why the last pass failed
	Findings []Finding   `json:"findings,omitempty"`
	// Dismissed are findings you put away: never shown or proposed again.
	Dismissed []string `json:"dismissed,omitempty"`
}

// Dir is where the advisor keeps its record, and where its passes run.
func Dir() string { return filepath.Join(state.Dir(), "advisor") }

func recordPath() string { return filepath.Join(Dir(), "record.json") }

// Load reads the record; a missing one is empty.
func Load() *Record {
	r := &Record{}
	if b, err := os.ReadFile(recordPath()); err == nil {
		_ = json.Unmarshal(b, r)
	}
	return r
}

// Save writes the record.
func (r *Record) Save() error {
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp := recordPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, recordPath())
}

// Due is whether a pass should run now, given how many requests your
// agents have made since the last one.
func (r *Record) Due(now time.Time, newReqs int64) bool {
	if r.LastRun.IsZero() {
		return newReqs > 0
	}
	return now.Sub(r.LastRun) >= Gap && newReqs >= MinRequests
}

// ReviewsLeft is how many Opus reviews the day's cap still allows.
func (r *Record) ReviewsLeft(now time.Time) int {
	n := 0
	for _, t := range r.Reviews {
		if now.Sub(t) < 24*time.Hour {
			n++
		}
	}
	return max(0, ReviewsPerDay-n)
}

// Shown are the findings to show: confirmed first, then candidates, most
// at stake first within each.
func (r *Record) Shown() []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Status != Rejected && !slices.Contains(r.Dismissed, f.ID) {
			out = append(out, f)
		}
	}
	slices.SortStableFunc(out, func(a, b Finding) int {
		if (a.Status == Confirmed) != (b.Status == Confirmed) {
			if a.Status == Confirmed {
				return -1
			}
			return 1
		}
		switch {
		case a.Weekly > b.Weekly:
			return -1
		case a.Weekly < b.Weekly:
			return 1
		}
		return 0
	})
	return out
}

// Pending are the candidates still waiting on a review, not put away.
func (r *Record) Pending() []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Status == Candidate && !slices.Contains(r.Dismissed, f.ID) {
			out = append(out, f)
		}
	}
	return out
}

// Known are the titles a pass mustn't propose again: everything kept,
// rejected and dismissed included.
func (r *Record) Known() []string {
	var out []string
	for _, f := range r.Findings {
		out = append(out, f.Title)
	}
	return out
}

// Dismiss puts a finding away for good.
func (r *Record) Dismiss(id string) {
	if !slices.Contains(r.Dismissed, id) {
		r.Dismissed = append(r.Dismissed, id)
	}
}

// Merge takes a pass's result in, and reports the findings that are newly
// confirmed.
func (r *Record) Merge(res Result) []Finding {
	r.LastRun, r.Runs = res.At, r.Runs+1
	r.Spent += res.Spent
	r.Reviews = append(r.Reviews, res.Reviewed...)
	r.Err = ""
	if res.Err != nil {
		r.Err = res.Err.Error()
	}
	var fresh []Finding
	for _, f := range res.Findings {
		i := slices.IndexFunc(r.Findings, func(o Finding) bool { return o.ID == f.ID })
		if i < 0 {
			r.Findings = append(r.Findings, f)
		} else if r.Findings[i].Status != Confirmed {
			r.Findings[i] = f
		} else {
			continue
		}
		if f.Status == Confirmed && !slices.Contains(r.Dismissed, f.ID) {
			fresh = append(fresh, f)
		}
	}
	// Keep the newest, but rejected and dismissed ones only as long as
	// there's room: they're what stops a finding coming back.
	if len(r.Findings) > Keep {
		slices.SortStableFunc(r.Findings, func(a, b Finding) int { return b.At.Compare(a.At) })
		r.Findings = r.Findings[:Keep]
	}
	cut := time.Now().Add(-24 * time.Hour)
	r.Reviews = slices.DeleteFunc(r.Reviews, func(t time.Time) bool { return t.Before(cut) })
	return fresh
}

// idOf names a finding by its title, so the same one proposed twice is one.
func idOf(title string) string {
	s := strings.Join(strings.Fields(strings.ToLower(title)), " ")
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:6])
}
