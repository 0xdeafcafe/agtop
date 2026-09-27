package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The kinds of Draft: what you kept on purpose, what you sent, and what
// you cleared from the box without sending.
const (
	KindDraft   = "draft"
	KindSent    = "sent"
	KindCleared = "cleared"
)

// DraftKinds are the kinds in the order the drafts sheet shows them.
var DraftKinds = []string{KindDraft, KindSent, KindCleared}

// A Draft is something you typed into a box: kept as a draft, sent, or
// cleared before it was. Kept so a wiped box is never lost for good.
type Draft struct {
	Text  string    `json:"text"`
	At    time.Time `json:"at"`
	Agent string    `json:"agent,omitempty"` // the agent's key
	Name  string    `json:"name,omitempty"`  // and its name then
	Kind  string    `json:"kind,omitempty"`
	// Sent is what older agtops read; it's kept in step with Kind.
	Sent bool `json:"sent,omitempty"`
}

// maxKept is how many of each kind are kept, newest first.
var maxKept = map[string]int{KindDraft: 300, KindSent: 300, KindCleared: 300}

var (
	draftsMu sync.Mutex
	// What was last read or written, so counting them each frame costs
	// no read.
	draftCache struct {
		path    string
		list    []Draft
		checked time.Time
		mod     time.Time
	}
)

func draftsPath() string { return filepath.Join(Dir(), "drafts.json") }

// loadDrafts reads the drafts; the lock is held. Drafts from before there
// were kinds are given one, sent or cleared, and the file as it was is
// kept beside it as drafts.json.bak.
func loadDrafts() []Draft {
	path := draftsPath()
	b, err := os.ReadFile(path)
	var out []Draft
	if err == nil {
		_ = json.Unmarshal(b, &out)
	}
	migrated := false
	for i := range out {
		if out[i].Kind == "" {
			out[i].Kind = KindCleared
			if out[i].Sent {
				out[i].Kind = KindSent
			}
			migrated = true
		}
	}
	if migrated {
		if _, err := os.Stat(path + ".bak"); os.IsNotExist(err) {
			_ = os.WriteFile(path+".bak", b, 0o600)
		}
		_ = writeJSON(path, out)
	}
	setCache(path, out)
	return out
}

func setCache(path string, list []Draft) {
	draftCache.path, draftCache.list, draftCache.checked = path, list, time.Now()
	if fi, err := os.Stat(path); err == nil {
		draftCache.mod = fi.ModTime()
	} else {
		draftCache.mod = time.Time{}
	}
}

func saveDrafts(list []Draft) error {
	path := draftsPath()
	err := writeJSON(path, list)
	setCache(path, list)
	return err
}

// Drafts are the drafts kept of every kind, newest first.
func Drafts() []Draft {
	draftsMu.Lock()
	defer draftsMu.Unlock()
	return loadDrafts()
}

// DraftsOf are the drafts of one kind, newest first.
func DraftsOf(kind string) []Draft {
	var out []Draft
	for _, d := range Drafts() {
		if d.Kind == kind {
			out = append(out, d)
		}
	}
	return out
}

// DraftCount is how many drafts of a kind are kept. It's cheap enough to
// ask every frame: the file is looked at again at most once a second.
func DraftCount(kind string) int {
	draftsMu.Lock()
	defer draftsMu.Unlock()
	path := draftsPath()
	list := draftCache.list
	if draftCache.path != path {
		list = loadDrafts()
	} else if time.Since(draftCache.checked) > time.Second {
		draftCache.checked = time.Now()
		var mod time.Time
		if fi, err := os.Stat(path); err == nil {
			mod = fi.ModTime()
		}
		if !mod.Equal(draftCache.mod) {
			list = loadDrafts() // another agtop saved some
		}
	}
	n := 0
	for _, d := range list {
		if d.Kind == kind {
			n++
		}
	}
	return n
}

// AddDraft keeps d at the top of its kind. The same text again moves up
// rather than being kept twice. A text is sent or not: sending it takes
// it out of the drafts and the cleared, and keeping it as a draft takes
// it out of the cleared. Clearing a text that's a draft leaves it one.
func AddDraft(d Draft) error {
	if strings.TrimSpace(d.Text) == "" {
		return nil
	}
	if d.Kind == "" {
		d.Kind = KindCleared
		if d.Sent {
			d.Kind = KindSent
		}
	}
	d.Sent = d.Kind == KindSent
	draftsMu.Lock()
	defer draftsMu.Unlock()
	old := loadDrafts()
	same := func(o Draft) bool { return strings.TrimSpace(o.Text) == strings.TrimSpace(d.Text) }
	if d.Kind == KindCleared {
		for _, o := range old {
			if o.Kind == KindDraft && same(o) {
				return nil
			}
		}
	}
	out := make([]Draft, 0, len(old)+1)
	out = append(out, d)
	n := map[string]int{d.Kind: 1}
	for _, o := range old {
		if same(o) && (o.Kind == d.Kind || d.Kind == KindSent || d.Kind == KindDraft && o.Kind == KindCleared) {
			continue
		}
		if limit := maxKept[o.Kind]; limit > 0 && n[o.Kind] >= limit {
			continue
		}
		n[o.Kind]++
		out = append(out, o)
	}
	return saveDrafts(out)
}

// RemoveDraft forgets the draft of this kind with this text.
func RemoveDraft(kind, text string) error {
	draftsMu.Lock()
	defer draftsMu.Unlock()
	old := loadDrafts()
	out := make([]Draft, 0, len(old))
	for _, o := range old {
		if o.Text != text || o.Kind != kind {
			out = append(out, o)
		}
	}
	return saveDrafts(out)
}
