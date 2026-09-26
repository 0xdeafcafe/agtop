package state

import (
	"encoding/hex"
	"os"
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

// BoxDraft is what a session's message box holds while you write it: the
// text, where the cursor is, and the long pastes and images its text
// stands for. It is kept on disk as you write, so closing agtop, or the
// terminal it's in, loses none of it; the drafts above are what was sent
// or cleared.
type BoxDraft struct {
	Text   string         `json:"text,omitempty"`
	Back   int            `json:"back,omitempty"` // the cursor, in runes from the end
	PasteN int            `json:"pasteN,omitempty"`
	Pastes map[int]string `json:"pastes,omitempty"`
	ImageN int            `json:"imageN,omitempty"`
	Images map[int]string `json:"images,omitempty"`
	// Stash is a message put aside to write another; it comes back once
	// that one is sent.
	Stash *BoxDraft `json:"stash,omitempty"`
	At    time.Time `json:"at"`
}

// Empty says there's nothing in it to keep.
func (d BoxDraft) Empty() bool { return strings.TrimSpace(d.Text) == "" && d.Stash == nil }

var boxDraftMu sync.Mutex

// boxDraftPath is a session's draft file. The key, an agent's, is hex so
// it is always a plain file name.
func boxDraftPath(key string) string {
	return filepath.Join(Dir(), "box-drafts", hex.EncodeToString([]byte(key))+".json")
}

// ReadBoxDraft is the draft kept for a session's box, if there is one.
func ReadBoxDraft(key string) (BoxDraft, bool) {
	boxDraftMu.Lock()
	defer boxDraftMu.Unlock()
	var d BoxDraft
	readJSON(boxDraftPath(key), &d)
	return d, !d.Empty()
}

// SaveBoxDraft keeps a session's draft, written whole or not at all. An
// empty one removes the file.
func SaveBoxDraft(key string, d BoxDraft) error {
	boxDraftMu.Lock()
	defer boxDraftMu.Unlock()
	if d.Empty() {
		if err := os.Remove(boxDraftPath(key)); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return writeJSON(boxDraftPath(key), d)
}
