// Package drafts is rush's stash, as a plugin bundled with it: a message
// box is kept until it's sent, a message can be stashed while you send
// another, and what you sent, cleared and replaced can be put back. What
// it's called, Stash or Drafts, is a setting: see Words.
//
// book.go is the logic, with no I/O and no clock of its own; plugin.go
// wires it to rush.
package drafts

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/0xdeafcafe/rush/internal/plugin"
)

// Kinds of what's kept.
const (
	Sent     = "sent"    //
	Cleared  = "cleared" // wiped from a box without sending
	Replaced = "kept"    // a box's text a put-back replaced; "kept" on disk
)

// Tabs are the pick's, in order, after the first: the stashes, by Words.
var Tabs = []string{"Sent", "Cleared", "Replaced"}

var kindTab = map[string]int{Sent: 1, Cleared: 2, Replaced: 3}

// Words are what it's called in everything it and rush say, by its called
// setting.
type Words struct {
	Title string // the history's: Stash
	Tab   string // the stashes' tab: Stashed
	Verb  string // what the stash key does: stash
	Did   string // what it did: stashed
	Noun  string // one set aside: your stash
}

// Called are the names it can go by; the first is the default.
var Called = []string{"Stash", "Drafts"}

var words = map[string]Words{
	"Stash":  {Title: "Stash", Tab: "Stashed", Verb: "stash", Did: "stashed", Noun: "stash"},
	"Drafts": {Title: "Drafts", Tab: "Drafts", Verb: "keep as a draft", Did: "kept as a draft", Noun: "draft"},
}

// WordsFor is the words for a called setting, Stash's for any other.
func WordsFor(called string) Words {
	if w, ok := words[called]; ok {
		return w
	}
	return words[Called[0]]
}

// Command is the # command rush opens the history with: #stash.
func (w Words) Command() string { return "#" + strings.ToLower(w.Title) }

// Fill puts the words in s where it says {title}, {tab}, {verb}, {did},
// {noun} or {command}.
func (w Words) Fill(s string) string {
	return strings.NewReplacer("{title}", w.Title, "{tab}", w.Tab, "{verb}", w.Verb, "{did}", w.Did, "{noun}", w.Noun,
		"{command}", w.Command()).Replace(s)
}

// Entry is something typed into a box, kept.
type Entry struct {
	ID      string     `json:"id"`
	Kind    string     `json:"kind"`
	Box     plugin.Box `json:"box"`
	Session string     `json:"session,omitempty"` // the box's: "" is the Prompt
	Name    string     `json:"name,omitempty"`    // what that session was called
	At      time.Time  `json:"at"`
}

// Stash is a box set aside.
type Stash struct {
	Box  plugin.Box `json:"box"`
	Name string     `json:"name,omitempty"`
	At   time.Time  `json:"at"`
}

// Store is what's kept on disk.
type Store struct {
	// Boxes are what each box held, until it's sent or emptied.
	Boxes   map[string]plugin.Box `json:"boxes,omitempty"`
	Stashes map[string]Stash      `json:"stashes,omitempty"`
	History []Entry               `json:"history,omitempty"` // newest first
	Seq     int                   `json:"seq,omitzero"`
	// Imported is that the drafts rush kept itself, before, are taken in.
	Imported bool `json:"imported,omitzero"`
}

// Book keeps the store, and says what to do to rush's boxes. What it says
// names rush's stash key as {aside} and its history key as {history}, for
// rush to fill in with the keys you have.
type Book struct {
	S    Store
	W    Words
	keep int // entries kept of each kind
}

// DefaultKeep is how many of each kind are kept.
const DefaultKeep = 300

// NewBook is a book over s, keeping keep of each kind.
func NewBook(s Store, keep int) *Book {
	if s.Boxes == nil {
		s.Boxes = map[string]plugin.Box{}
	}
	if s.Stashes == nil {
		s.Stashes = map[string]Stash{}
	}
	if keep <= 0 {
		keep = DefaultKeep
	}
	return &Book{S: s, W: WordsFor(""), keep: keep}
}

// ParseKeep reads the keep setting.
func ParseKeep(v string) int {
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return DefaultKeep
	}
	return n
}

// Empty is a box with nothing in it.
func Empty(b plugin.Box) bool { return strings.TrimSpace(b.Text) == "" && len(b.Images) == 0 }

// Set is a box to set: key's, to Box, only while it holds If.
type Set struct {
	Key string
	Box plugin.Box
	If  string
}

// Changed notes what a box holds now, saying whether that's its stash
// back: a stash goes only once the box is seen holding it, so one rush
// couldn't put back is never lost.
func (b *Book) Changed(key string, box plugin.Box) (back bool) {
	if Empty(box) {
		delete(b.S.Boxes, key)
		return false
	}
	b.S.Boxes[key] = box
	if st, ok := b.S.Stashes[key]; ok && strings.TrimSpace(st.Box.Text) == strings.TrimSpace(box.Text) {
		delete(b.S.Stashes, key)
		return true
	}
	return false
}

// Opened is a box coming into view: what it held, if rush's is empty.
func (b *Book) Opened(key string) (Set, bool) {
	box, ok := b.S.Boxes[key]
	if !ok {
		return Set{}, false
	}
	return Set{Key: key, Box: box, If: ""}, true
}

// Sent is a box's message gone: kept as sent, and the box's stash, if it
// has one, put back in the box it left empty.
func (b *Book) Sent(key, name, text string, now time.Time) (Set, bool) {
	// The box as last seen has its chips; the text sent, only when there
	// was none.
	box, ok := b.S.Boxes[key]
	if !ok {
		box = plugin.Box{Text: text, Cursor: len([]rune(text))}
	}
	delete(b.S.Boxes, key)
	if !Empty(box) {
		b.add(Sent, key, name, box, now)
	}
	st, ok := b.S.Stashes[key]
	if !ok {
		return Set{}, false
	}
	return Set{Key: key, Box: st.Box, If: ""}, true
}

// Cleared is a box wiped without sending, kept as cleared.
func (b *Book) Cleared(key, name, text string, now time.Time) {
	box, ok := b.S.Boxes[key]
	if !ok {
		box = plugin.Box{Text: text, Cursor: len([]rune(text))}
	}
	delete(b.S.Boxes, key)
	if !Empty(box) {
		b.add(Cleared, key, name, box, now)
	}
}

// said is what the book says, in its words.
func (b *Book) said(s string) string { return b.W.Fill(s) }

// StashResult is what stashing did.
type StashResult struct {
	Set  *Set
	Said string // what to tell you
}

// Stash is the stash key on a box holding box: what's typed is set aside
// and the box emptied; with nothing typed, what's stashed comes back; with
// both, the two change places.
func (b *Book) Stash(key, name string, box plugin.Box, now time.Time) StashResult {
	st, stashed := b.S.Stashes[key]
	switch {
	case Empty(box) && !stashed:
		return StashResult{Said: b.said("nothing to {verb} · type a message first")}
	case Empty(box):
		return StashResult{Set: &Set{Key: key, Box: st.Box, If: box.Text}, Said: b.said("your {noun} is back in the box")}
	case !stashed:
		b.S.Stashes[key] = Stash{Box: box, Name: name, At: now}
		delete(b.S.Boxes, key)
		// The Prompt's stash key is the list's own once it's empty.
		again := "{aside} brings it back now"
		if key == "" {
			again = "{history} › {tab} has it"
		}
		return StashResult{Set: &Set{Key: key, If: box.Text}, Said: b.said("{did} · it comes back after you send · " + again)}
	}
	b.S.Stashes[key] = Stash{Box: box, Name: name, At: now}
	return StashResult{Set: &Set{Key: key, Box: st.Box, If: box.Text}, Said: b.said("{did} this one, and your other {noun} is back · {aside} swaps them again")}
}

// Note is what a box's edge says: whether it has a stash.
func (b *Book) Note(key string) string {
	if _, ok := b.S.Stashes[key]; ok {
		return b.said("{did} · back after you send")
	}
	return ""
}

// PutBack puts an item back in the box of key, which holds cur (nil when
// it isn't the box with the keys: then only an empty one is set). What the
// box held is kept as replaced, so nothing is lost, and said says where.
func (b *Book) PutBack(id, key, name string, cur *plugin.Box, now time.Time) (set Set, said string, ok bool) {
	var box plugin.Box
	switch {
	case strings.HasPrefix(id, "stash:"):
		from := strings.TrimPrefix(id, "stash:")
		st, ok := b.S.Stashes[from]
		if !ok {
			return Set{}, "", false
		}
		delete(b.S.Stashes, from)
		box = st.Box
	default:
		i := slices.IndexFunc(b.S.History, func(e Entry) bool { return e.ID == id })
		if i < 0 {
			return Set{}, "", false
		}
		box = b.S.History[i].Box
	}
	if cur == nil {
		return Set{Key: key, Box: box, If: ""}, "", true
	}
	if !Empty(*cur) {
		b.add(Replaced, key, name, *cur, now)
		said = "put back · what the box held is under {history} › Replaced"
	}
	return Set{Key: key, Box: box, If: cur.Text}, said, true
}

// Forget drops an item.
func (b *Book) Forget(id string) {
	if from, ok := strings.CutPrefix(id, "stash:"); ok {
		delete(b.S.Stashes, from)
		return
	}
	b.S.History = slices.DeleteFunc(b.S.History, func(e Entry) bool { return e.ID == id })
}

// add keeps box as kind, newest first, dropping the same text of the same
// kind kept before, and the oldest past keep.
func (b *Book) add(kind, key, name string, box plugin.Box, now time.Time) {
	text := strings.TrimSpace(Plain(box))
	b.S.History = slices.DeleteFunc(b.S.History, func(e Entry) bool {
		return e.Kind == kind && strings.TrimSpace(Plain(e.Box)) == text
	})
	b.S.Seq++
	e := Entry{ID: "h" + strconv.Itoa(b.S.Seq), Kind: kind, Box: box, Session: key, Name: name, At: now}
	b.S.History = append([]Entry{e}, b.S.History...)
	b.trim()
}

// SetKeep changes how many are kept, saying whether any went.
func (b *Book) SetKeep(n int) bool {
	b.keep = n
	was := len(b.S.History)
	b.trim()
	return len(b.S.History) != was
}

func (b *Book) trim() {
	count := map[string]int{}
	b.S.History = slices.DeleteFunc(b.S.History, func(e Entry) bool {
		count[e.Kind]++
		return count[e.Kind] > b.keep
	})
}

// pasteRe is convo.PasteChipRe: a paste's chip, its number the first group.
var pasteRe = regexp.MustCompile(`\[(?:[Pp]asted text )?#(\d+) (?:· )?(?:\+\d+ lines|\d+ lines?(?:: [^\]\n]*)?)\]`)

// Plain is a box's text with its pastes put back, as it would be sent.
func Plain(b plugin.Box) string {
	if len(b.Pastes) == 0 {
		return b.Text
	}
	return pasteRe.ReplaceAllStringFunc(b.Text, func(chip string) string {
		n, _ := strconv.Atoi(pasteRe.FindStringSubmatch(chip)[1])
		if t, ok := b.Pastes[n]; ok {
			return t
		}
		return chip
	})
}

// Pick is the list of what's kept, for the box of key: the stashes, then
// each kind, newest first.
func (b *Book) Pick(key string, now time.Time) plugin.Pick {
	p := plugin.Pick{ID: "history", Title: b.W.Title, About: b.said("what you {did}, sent, cleared and replaced · enter puts one in the box"),
		Tabs: append([]string{b.W.Tab}, Tabs...), Session: key,
		Actions: []plugin.PickAction{{Key: "enter", Name: "put it in the box"}, {Key: "ctrl+d", Name: "forget it", Stay: true}},
		Empty: []string{b.said("nothing here · {aside} sets what's typed aside while you send another"),
			"nothing sent yet · the messages you send land here, to use again",
			"nothing cleared · what you wipe from a box lands here",
			"nothing replaced · what a box held when you put another back lands here"}}
	var stashed []string
	for k := range b.S.Stashes {
		stashed = append(stashed, k)
	}
	slices.SortFunc(stashed, func(x, y string) int { return b.S.Stashes[y].At.Compare(b.S.Stashes[x].At) })
	for _, k := range stashed {
		st := b.S.Stashes[k]
		p.Items = append(p.Items, plugin.PickItem{ID: "stash:" + k, Tab: 0, Text: Plain(st.Box), Meta: meta(st.Name, k, st.At, now)})
	}
	for _, e := range b.S.History {
		if len(p.Items) >= plugin.MaxPickItems {
			break
		}
		p.Items = append(p.Items, plugin.PickItem{ID: e.ID, Tab: kindTab[e.Kind], Text: Plain(e.Box), Meta: meta(e.Name, e.Session, e.At, now)})
	}
	// It opens on what you did last, a minute ago or less; else on this
	// box's stash, or on what you sent.
	switch _, ok := b.S.Stashes[key]; {
	case len(b.S.History) > 0 && now.Sub(b.S.History[0].At) < time.Minute:
		p.Tab = kindTab[b.S.History[0].Kind]
	case ok:
		p.Tab = 0
	case len(stashed) > 0:
		p.Tab = 0
	default:
		p.Tab = kindTab[Sent]
	}
	return p
}

func meta(name, key string, at, now time.Time) string {
	if name == "" && key == "" {
		name = "the Prompt"
	}
	s := ago(now.Sub(at))
	if name != "" {
		r := []rune(name)
		if len(r) > 22 {
			name = string(r[:21]) + "…"
		}
		s = name + " · " + s
	}
	return s
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}
