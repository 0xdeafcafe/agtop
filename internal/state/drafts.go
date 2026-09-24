package state

import (
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// A Draft is something you typed into a box: a message sent, or one
// cleared before it was. Kept so a wiped box is never lost for good.
type Draft struct {
	Text  string    `json:"text"`
	At    time.Time `json:"at"`
	Agent string    `json:"agent,omitempty"` // the agent's key
	Name  string    `json:"name,omitempty"`  // and its name then
	Sent  bool      `json:"sent,omitempty"`
}

// maxDrafts is how many are kept, newest first.
const maxDrafts = 300

var draftsMu sync.Mutex

func draftsPath() string { return filepath.Join(Dir(), "drafts.json") }

// Drafts are the drafts kept, newest first.
func Drafts() []Draft {
	draftsMu.Lock()
	defer draftsMu.Unlock()
	var out []Draft
	readJSON(draftsPath(), &out)
	return out
}

// AddDraft keeps d at the top. The same text again moves to the top
// rather than being kept twice.
func AddDraft(d Draft) error {
	if strings.TrimSpace(d.Text) == "" {
		return nil
	}
	draftsMu.Lock()
	defer draftsMu.Unlock()
	var old []Draft
	readJSON(draftsPath(), &old)
	out := make([]Draft, 0, len(old)+1)
	out = append(out, d)
	for _, o := range old {
		if strings.TrimSpace(o.Text) != strings.TrimSpace(d.Text) {
			out = append(out, o)
		}
	}
	return writeJSON(draftsPath(), out[:min(len(out), maxDrafts)])
}

// RemoveDraft forgets the draft with this text.
func RemoveDraft(text string) error {
	draftsMu.Lock()
	defer draftsMu.Unlock()
	var old []Draft
	readJSON(draftsPath(), &old)
	out := old[:0]
	for _, o := range old {
		if o.Text != text {
			out = append(out, o)
		}
	}
	return writeJSON(draftsPath(), out)
}
