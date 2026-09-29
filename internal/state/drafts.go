package state

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/0xdeafcafe/rush/internal/jsonx"
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
	// Sent is what older rushes read; it's kept in step with Kind.
	Sent bool `json:"sent,omitzero"`
}

// maxKept is how many of each kind are kept, newest first.
var maxKept = map[string]int{KindDraft: 300, KindSent: 300, KindCleared: 300}

var (
	// draftsMu is held while the file is read or written.
	draftsMu sync.Mutex
	// cacheMu guards draftCache, and is never held across a read or a
	// write, so the UI can ask it for what's kept without waiting.
	cacheMu sync.Mutex
	// What was last read or written, so counting them each frame costs
	// no read.
	draftCache struct {
		path    string
		list    []Draft
		loaded  bool
		checked time.Time
		mod     time.Time
		count   map[string]int
		// refreshing says a look at the file is on the queue.
		refreshing bool
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
		_ = jsonx.Unmarshal(b, &out)
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

func modTime(path string) time.Time {
	if fi, err := os.Stat(path); err == nil {
		return fi.ModTime()
	}
	return time.Time{}
}

// setCache keeps list as what's on disk at path. The file's time is read
// before the memory lock is taken.
func setCache(path string, list []Draft) {
	mod := modTime(path)
	cacheMu.Lock()
	defer cacheMu.Unlock()
	putCache(path, list)
	draftCache.mod = mod
}

// putCache sets the memory side; cacheMu is held.
func putCache(path string, list []Draft) {
	draftCache.path, draftCache.list, draftCache.loaded, draftCache.checked = path, list, true, time.Now()
	if draftCache.count == nil {
		draftCache.count = map[string]int{}
	}
	clear(draftCache.count)
	for _, d := range list {
		draftCache.count[d.Kind]++
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

// DraftCount is how many drafts of a kind are kept. It reads the file
// again when another rush may have changed it; KeptCount is the one that
// never waits.
func DraftCount(kind string) int {
	path := draftsPath()
	cacheMu.Lock()
	fresh := draftCache.loaded && draftCache.path == path && time.Since(draftCache.checked) <= time.Second
	n := draftCache.count[kind]
	cacheMu.Unlock()
	if fresh {
		return n
	}
	refreshDrafts()
	cacheMu.Lock()
	defer cacheMu.Unlock()
	return draftCache.count[kind]
}

// refreshDrafts reads the file again if it changed since it was last read.
func refreshDrafts() {
	draftsMu.Lock()
	defer draftsMu.Unlock()
	path := draftsPath()
	mod := modTime(path)
	cacheMu.Lock()
	same := draftCache.loaded && draftCache.path == path && mod.Equal(draftCache.mod)
	if same {
		draftCache.checked = time.Now()
	}
	cacheMu.Unlock()
	if !same {
		loadDrafts() // another rush saved some
	}
}

// --- without waiting ---

// Kept is the drafts of every kind as last read or written, newest first,
// from memory: it never touches the disk. ok is false until they've been
// read, which asking starts in the background. The list is shared: don't
// change it.
func Kept() (list []Draft, ok bool) {
	path := draftsPath()
	cacheMu.Lock()
	if draftCache.path != path {
		draftCache.loaded = false // RUSH_HOME moved, as in tests
	}
	look := lookDue()
	list, ok = draftCache.list, draftCache.loaded
	cacheMu.Unlock()
	if look {
		lookLater()
		if !ok { // looked at once, when RunLater doesn't wait
			cacheMu.Lock()
			list, ok = draftCache.list, draftCache.loaded
			cacheMu.Unlock()
		}
	}
	return list, ok
}

// KeptCount is how many drafts of a kind are kept, from memory: cheap
// enough to ask every frame. At most once a second it has the file looked
// at again in the background, for what another rush saved.
func KeptCount(kind string) int {
	cacheMu.Lock()
	look := lookDue()
	n := draftCache.count[kind]
	cacheMu.Unlock()
	if look {
		lookLater()
		cacheMu.Lock()
		n = draftCache.count[kind]
		cacheMu.Unlock()
	}
	return n
}

// lookDue is whether the file is due a look, when the memory side is
// over a second old, and marks one as queued; cacheMu is held.
func lookDue() bool {
	if draftCache.refreshing || draftCache.loaded && time.Since(draftCache.checked) <= time.Second {
		return false
	}
	draftCache.refreshing = true
	return true
}

// lookLater queues a look at the file.
func lookLater() {
	later(func() {
		refreshDrafts()
		cacheMu.Lock()
		draftCache.refreshing = false
		cacheMu.Unlock()
	})
}

// KeepLater is AddDraft without the wait: what's kept in memory has d at
// once, and the file is written in the background, in order.
func KeepLater(d Draft) {
	d, ok := normal(d)
	if !ok {
		return
	}
	path := draftsPath()
	cacheMu.Lock()
	if draftCache.loaded && draftCache.path == path {
		if list, changed := withDraft(draftCache.list, d); changed {
			putCache(draftCache.path, list)
		}
	}
	cacheMu.Unlock()
	later(func() { _ = AddDraft(d) })
}

// ForgetLater is RemoveDraft without the wait.
func ForgetLater(kind, text string) {
	path := draftsPath()
	cacheMu.Lock()
	if draftCache.loaded && draftCache.path == path {
		putCache(draftCache.path, without(draftCache.list, kind, text))
	}
	cacheMu.Unlock()
	later(func() { _ = RemoveDraft(kind, text) })
}

// SettleDrafts waits for the drafts' background writes and reads.
func SettleDrafts() {
	for {
		queue.Lock()
		idle := !queue.running
		queue.Unlock()
		if idle {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

// queue runs the drafts' background work one at a time, in the order it
// was asked for.
var queue struct {
	sync.Mutex
	ops     []func()
	running bool
}

// RunLater runs the drafts' background work. Tests make it run at once,
// so the drafts are on disk when the call that kept one returns.
var RunLater = func(f func()) { go f() }

func later(op func()) {
	queue.Lock()
	queue.ops = append(queue.ops, op)
	start := !queue.running
	queue.running = true
	queue.Unlock()
	if start {
		RunLater(drain)
	}
}

func drain() {
	for {
		queue.Lock()
		if len(queue.ops) == 0 {
			queue.running = false
			queue.Unlock()
			return
		}
		op := queue.ops[0]
		queue.ops = queue.ops[1:]
		queue.Unlock()
		op()
	}
}

// normal fills in d's kind; ok is false for an empty one.
func normal(d Draft) (Draft, bool) {
	if strings.TrimSpace(d.Text) == "" {
		return d, false
	}
	if d.Kind == "" {
		d.Kind = KindCleared
		if d.Sent {
			d.Kind = KindSent
		}
	}
	d.Sent = d.Kind == KindSent
	return d, true
}

// withDraft is old with d kept at the top of its kind: see AddDraft.
// changed is false when it's kept as it was.
func withDraft(old []Draft, d Draft) (list []Draft, changed bool) {
	same := func(o Draft) bool { return strings.TrimSpace(o.Text) == strings.TrimSpace(d.Text) }
	if d.Kind == KindCleared {
		for _, o := range old {
			if o.Kind == KindDraft && same(o) {
				return old, false
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
	return out, true
}

func without(old []Draft, kind, text string) []Draft {
	out := make([]Draft, 0, len(old))
	for _, o := range old {
		if o.Text != text || o.Kind != kind {
			out = append(out, o)
		}
	}
	return out
}

// AddDraft keeps d at the top of its kind. The same text again moves up
// rather than being kept twice. A text is sent or not: sending it takes
// it out of the drafts and the cleared, and keeping it as a draft takes
// it out of the cleared. Clearing a text that's a draft leaves it one.
func AddDraft(d Draft) error {
	d, ok := normal(d)
	if !ok {
		return nil
	}
	draftsMu.Lock()
	defer draftsMu.Unlock()
	old := loadDrafts()
	out, changed := withDraft(old, d)
	if !changed {
		return nil // a cleared one that's a draft already
	}
	return saveDrafts(out)
}

// RemoveDraft forgets the draft of this kind with this text.
func RemoveDraft(kind, text string) error {
	draftsMu.Lock()
	defer draftsMu.Unlock()
	return saveDrafts(without(loadDrafts(), kind, text))
}
