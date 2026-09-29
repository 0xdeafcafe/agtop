package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Debounce is how long a box must stay unchanged before what's in it is
// written down as its autosave.
const Debounce = 1500 * time.Millisecond

// DefaultKeep is how many drafts a box keeps when the setting says nothing
// it understands.
const DefaultKeep = 50

// Draft is a message you typed and didn't send.
type Draft struct {
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

// Store is everything the plugin keeps, by box: a Session's box by its
// rush session id, the Prompt's as "".
type Store struct {
	// Autosave is what each box held when it last settled, until it's
	// sent, or kept as a draft.
	Autosave map[string]Draft `json:"autosave,omitempty"`
	// Drafts are each box's drafts, oldest first.
	Drafts map[string][]Draft `json:"drafts,omitempty"`
}

// pending is a box's text not yet settled.
type pending struct {
	text string
	at   time.Time // when it last changed
}

// Book is the plugin's pure state: what it knows of each box, and what it
// keeps. It does no I/O and reads no clock; callers pass the time in.
type Book struct {
	Store
	keep    int
	pending map[string]pending
}

// NewBook starts from what was kept, keeping at most keep drafts a box.
func NewBook(s Store, keep int) *Book {
	if s.Autosave == nil {
		s.Autosave = map[string]Draft{}
	}
	if s.Drafts == nil {
		s.Drafts = map[string][]Draft{}
	}
	b := &Book{Store: s, pending: map[string]pending{}}
	b.SetKeep(keep)
	return b
}

// ParseKeep reads the "keep" setting.
func ParseKeep(v string) int {
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return n
	}
	return DefaultKeep
}

// SetKeep changes how many drafts a box keeps, dropping the oldest beyond
// it. It reports whether anything was dropped.
func (b *Book) SetKeep(n int) bool {
	if n <= 0 {
		n = DefaultKeep
	}
	b.keep = n
	dropped := false
	for k, ds := range b.Drafts {
		if len(ds) > n {
			b.Drafts[k] = append([]Draft(nil), ds[len(ds)-n:]...)
			dropped = true
		}
	}
	return dropped
}

// Changed notes what box key holds now. Nothing is written until it
// settles (see Settle).
func (b *Book) Changed(key, text string, now time.Time) {
	b.pending[key] = pending{text: text, at: now}
}

// Settle moves each box that hasn't changed for Debounce into its
// autosave, and says whether the store changed. An emptied box drops its
// autosave.
func (b *Book) Settle(now time.Time) bool {
	changed := false
	for k, p := range b.pending {
		if now.Sub(p.at) < Debounce {
			continue
		}
		delete(b.pending, k)
		if p.text == "" {
			if _, ok := b.Autosave[k]; ok {
				delete(b.Autosave, k)
				changed = true
			}
			continue
		}
		if a, ok := b.Autosave[k]; ok && a.Text == p.text {
			continue
		}
		b.Autosave[k] = Draft{Text: p.text, At: p.at}
		changed = true
	}
	return changed
}

// NextSettle is when Settle next has something to do, or zero.
func (b *Book) NextSettle() time.Time {
	var next time.Time
	for _, p := range b.pending {
		if t := p.at.Add(Debounce); next.IsZero() || t.Before(next) {
			next = t
		}
	}
	return next
}

// Keep saves what box key holds, or text if it's given (a cleared box
// says what it held), as a draft, and forgets the box's autosave. It says
// whether the store changed.
func (b *Book) Keep(key, text string, now time.Time) bool {
	if text == "" {
		if p, ok := b.pending[key]; ok {
			text = p.text
		} else {
			text = b.Autosave[key].Text
		}
	}
	delete(b.pending, key)
	_, had := b.Autosave[key]
	delete(b.Autosave, key)
	if text == "" {
		return had
	}
	ds := b.Drafts[key]
	if n := len(ds); n > 0 && ds[n-1].Text == text {
		ds[n-1].At = now // the same draft again: only newer
		return true
	}
	ds = append(ds, Draft{Text: text, At: now})
	if len(ds) > b.keep {
		ds = ds[len(ds)-b.keep:]
	}
	b.Drafts[key] = ds
	return true
}

// Sent forgets box key's unsettled text and autosave: it went. It says
// whether the store changed.
func (b *Book) Sent(key string) bool {
	delete(b.pending, key)
	if _, ok := b.Autosave[key]; ok {
		delete(b.Autosave, key)
		return true
	}
	return false
}

// Restore takes box key's newest draft out, to put it back in the box; or
// its autosave, when it has no draft (rush quit with it typed). Taking it
// out means restoring again goes one further back, and a restored draft
// cleared again comes back as the newest.
func (b *Book) Restore(key string) (string, bool) {
	if ds := b.Drafts[key]; len(ds) > 0 {
		d := ds[len(ds)-1]
		if len(ds) == 1 {
			delete(b.Drafts, key)
		} else {
			b.Drafts[key] = ds[:len(ds)-1]
		}
		return d.Text, true
	}
	if a, ok := b.Autosave[key]; ok {
		delete(b.Autosave, key)
		return a.Text, true
	}
	return "", false
}

// Count is how many drafts box key keeps.
func (b *Book) Count(key string) int { return len(b.Drafts[key]) }

// load reads the store from path; a missing or broken file is an empty one.
func load(path string) Store {
	var s Store
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	return s
}

// save writes the store to path atomically: a temporary file beside it,
// synced, then renamed over it. Only the owner can read it.
func save(path string, s Store) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return writeAtomic(path, data)
}

// writeAtomic writes data to path so a reader sees the old file or the
// new one, never half of it.
func writeAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".drafts-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // gone after the rename; cleans up on failure
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
