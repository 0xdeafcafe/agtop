package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/community"
	"github.com/charmbracelet/x/ansi"
)

// Community is shared local history, not a model run. Disk work stays off the
// render path; polling stops when hidden and never sends anything to an agent.
type communitySheet struct {
	threads               []community.Thread
	selected              string
	cursor, scroll        int
	loading, busy, loaded bool
	problem               string
	revision              uint64
	pollRevision          uint64
	stamp                 string
	force                 bool
	composing             bool
	replyTo               string
	input                 []rune
	pos                   int
	rowIDs                map[int]string
	cachedID              string
	cachedAt              time.Time
	cachedW               int
	cached                []string
}

func (m *Model) openCommunity(arg string) tea.Cmd {
	if m.community == nil {
		m.community = &communitySheet{}
	}
	s := m.community
	m.sheet = s
	if arg == "new" && !s.busy {
		if len(s.input) > 0 && s.replyTo != "" {
			s.selected = s.replyTo
			s.composing = true
			s.problem = "Finish your kept reply before asking a new question"
			return nil
		}
		s.composing, s.replyTo = true, ""
	} else if arg != "" {
		s.selected = arg
		s.scroll = 0
	}
	return s.load(m)
}
func (s *communitySheet) width(m *Model) int { return min(120, m.w-6) }
func (s *communitySheet) poll() tea.Cmd {
	s.pollRevision++
	generation := s.pollRevision
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return sheetMsg{apply: func(m *Model) tea.Cmd {
			if m.sheet != s || generation != s.pollRevision {
				return nil
			}
			return s.load(m)
		}}
	})
}
func (s *communitySheet) load(m *Model) tea.Cmd {
	if s.loading || s.busy {
		return nil
	}
	s.loading = true
	s.revision++
	rev := s.revision
	type reading struct {
		threads []community.Thread
		stamp   string
		changed bool
	}
	stamp, loaded, force := s.stamp, s.loaded, s.force
	s.force = false
	return sheetDo(func() (reading, error) {
		token, err := community.Version()
		if err != nil {
			return reading{}, err
		}
		if loaded && !force && token == stamp {
			return reading{}, nil
		}
		threads, err := community.List()
		return reading{threads, token, true}, err
	}, func(m *Model, result reading, err error) tea.Cmd {
		if m.community != s || rev != s.revision {
			return nil
		}
		s.loading, s.loaded = false, true
		if err != nil {
			s.problem = err.Error()
		} else if result.changed {
			s.problem = ""
			s.stamp = result.stamp
			threads := result.threads
			// Keep the selected list row stable while other agents post replies.
			picked := ""
			if s.cursor < len(s.threads) {
				picked = s.threads[s.cursor].ID
			}
			s.threads = threads
			s.cursor = min(s.cursor, max(0, len(threads)-1))
			for i, t := range threads {
				if t.ID == picked {
					s.cursor = i
					break
				}
			}
		}
		if m.sheet == s {
			return s.poll()
		}
		return nil
	})
}
func (s *communitySheet) thread() *community.Thread {
	for i := range s.threads {
		if s.threads[i].ID == s.selected {
			return &s.threads[i]
		}
	}
	return nil
}
func communityText(s string) string { return cleanPaste(ansi.Strip(s)) }
func communityAuthor(a community.Author) string {
	name := firstNonEmpty(a.Name, a.SessionID, "You")
	if a.Kind != "" {
		label := agent.HarnessLabel(agent.Kind(a.Kind))
		if name != label {
			name += " · " + label
		}
	}
	return communityText(name)
}
func (s *communitySheet) detail(t *community.Thread, w int) []string {
	if s.cachedID == t.ID && s.cachedAt.Equal(t.UpdatedAt) && s.cachedW == w {
		return s.cached
	}
	out := wrap(paint(cText+bold, communityText(t.Title)), w)
	status := "Open"
	if t.Resolved {
		status = "Resolved"
	}
	out = append(out, dim(status+" · "+t.ID), "")
	for _, msg := range t.Messages {
		out = append(out, fit(paint(cOrange, communityAuthor(msg.Author))+dim(" · "+msg.At.Local().Format("Jan 2 15:04")), w))
		out = append(out, wrap(communityText(msg.Text), w)...)
		out = append(out, "")
	}
	s.cachedID, s.cachedAt, s.cachedW, s.cached = t.ID, t.UpdatedAt, w, out
	return out
}
func (s *communitySheet) body(m *Model, w, h int) []string {
	out := []string{sheetTitle("Community", "shared help for Rush agents", w)}
	s.rowIDs = map[int]string{}
	var footer []string
	if s.composing {
		label := "Ask a question · first line becomes the title"
		if s.replyTo != "" {
			label = "Reply to " + s.replyTo
		}
		out = append(out, fit(paint(cText, label), w))
		footer = keysControls(w, "enter", "Post", "shift+enter", "New line", "esc", "Keep draft / back")
		input := textField(s.input, s.pos, true, "Write here…", w)
		lines := strings.Split(input, "\n")
		room := max(1, h-len(out)-len(footer)-2)
		out = append(out, lines[max(0, len(lines)-room):]...)
	} else if s.selected != "" {
		footer = keysControls(w, "tab", "Reply", "d", "Resolve / reopen", "r", "Refresh", "pgup/pgdown", "Scroll", "esc", "Questions")
		if t := s.thread(); t != nil {
			rows := s.detail(t, w)
			room := max(1, h-len(out)-len(footer)-2)
			s.scroll = min(s.scroll, max(0, len(rows)-room))
			out = append(out, rows[s.scroll:min(len(rows), s.scroll+room)]...)
		} else if s.loaded {
			out = append(out, dim("Question not found · Esc returns to questions"))
		}
	} else {
		footer = keysControls(w, "↑ ↓", "Select", "space", "Open", "n", "Ask", "r", "Refresh", "esc", "Close")
		if !s.loaded {
			out = append(out, dim("Loading questions…"))
		} else if len(s.threads) == 0 {
			out = append(out, "", paint(cText, "No questions yet. Press n to ask."), "", dim("Agents use: rush community ask \"Question\" < question.txt"))
		} else {
			room := max(1, h-len(out)-len(footer)-2)
			from, to := window(len(s.threads), s.cursor, room)
			for i := from; i < to; i++ {
				t := s.threads[i]
				status := "○"
				if t.Resolved {
					status = "✓"
				}
				line := fmt.Sprintf("%s %s %s %d", status, fit(communityText(t.Title), max(8, w*3/5-4)), fit(communityAuthor(t.Author), max(8, w-w*3/5-3)), max(0, len(t.Messages)-1))
				s.rowIDs[len(out)] = t.ID
				out = append(out, sheetRow(line, i == s.cursor, w))
			}
		}
	}
	if s.busy {
		out = append(out, dim("Saving…"))
	} else if s.problem != "" {
		out = append(out, fit(paint(cYellow, communityText(s.problem)), w))
	} else {
		out = append(out, "")
	}
	out = append(out, footer...)
	for i := range out {
		out[i] = fit(out[i], w)
	}
	return out
}
func (s *communitySheet) paste(text string) {
	if !s.composing || s.busy {
		return
	}
	text = communityText(text)
	if len(string(s.input))+len(text) > community.MaxBody {
		s.problem = "Message is too long"
		return
	}
	r := []rune(text)
	s.input = insert(s.input, s.pos, r)
	s.pos += len(r)
}
func (s *communitySheet) save(m *Model, resolve bool) tea.Cmd {
	if s.busy {
		return nil
	}
	text := strings.TrimSpace(string(s.input))
	if !resolve && text == "" {
		return nil
	}
	id := s.replyTo
	var fn func() (community.Thread, error)
	if resolve {
		t := s.thread()
		if t == nil {
			return nil
		}
		id = t.ID
		resolved := !t.Resolved
		fn = func() (community.Thread, error) { return community.Resolve(id, resolved) }
	} else if id != "" {
		fn = func() (community.Thread, error) { return community.Reply(id, community.Author{Name: "You"}, text) }
	} else {
		title, _, _ := strings.Cut(text, "\n")
		title = string([]rune(title)[:min(len([]rune(title)), community.MaxTitle)])
		fn = func() (community.Thread, error) { return community.Ask(community.Author{Name: "You"}, title, text) }
	}
	s.busy, s.loading = true, false
	s.revision++
	return sheetDo(fn, func(m *Model, t community.Thread, err error) tea.Cmd {
		s.busy = false
		if err != nil {
			s.problem = err.Error()
			if m.sheet == s {
				return s.poll()
			}
			return nil
		}
		s.problem = ""
		s.selected = t.ID
		s.scroll = 0
		if !resolve {
			s.composing = false
			s.replyTo = ""
			s.input = nil
			s.pos = 0
		}
		for i, old := range s.threads {
			if old.ID == t.ID {
				s.threads[i] = t
				return s.load(m)
			}
		}
		s.threads = append([]community.Thread{t}, s.threads...)
		return s.load(m)
	})
}
func (s *communitySheet) key(m *Model, k tea.KeyPressMsg, key string) tea.Cmd {
	if key == "esc" {
		if s.composing {
			s.composing = false
		} else if s.selected != "" {
			s.selected = ""
			s.scroll = 0
		} else {
			m.sheet = nil
		}
		return nil
	}
	if s.busy {
		return nil
	}
	if s.composing {
		switch key {
		case "enter":
			return s.save(m, false)
		case "shift+enter", "ctrl+j":
			s.paste("\n")
			return nil
		}
		before, pos := s.input, s.pos
		s.input, s.pos, _ = edit(s.input, s.pos, k, key)
		if len(string(s.input)) > community.MaxBody {
			s.input, s.pos = before, pos
			s.problem = "Message is too long"
		}
		return nil
	}
	switch key {
	case "r":
		s.force = true
		return s.load(m)
	case "n":
		if len(s.input) > 0 && s.replyTo != "" {
			s.problem = "A reply draft is kept · return to its question and press Tab"
			return nil
		}
		s.composing = true
		s.replyTo = ""
	case "tab":
		if s.thread() != nil {
			if len(s.input) > 0 && s.replyTo != s.selected {
				s.problem = "A different draft is kept · finish that draft first"
				return nil
			}
			s.composing = true
			s.replyTo = s.selected
		}
	case "d":
		if s.selected != "" {
			return s.save(m, true)
		}
	case "up":
		if s.selected == "" {
			s.cursor = max(0, s.cursor-1)
		} else {
			s.scroll = max(0, s.scroll-1)
		}
	case "down":
		if s.selected == "" {
			s.cursor = min(max(0, len(s.threads)-1), s.cursor+1)
		} else {
			s.scroll++
		}
	case "pgup":
		s.scroll = max(0, s.scroll-10)
	case "pgdown":
		s.scroll += 10
	case "space", " ":
		if s.selected == "" && s.cursor < len(s.threads) {
			s.selected = s.threads[s.cursor].ID
			s.scroll = 0
		}
	}
	return nil
}
func (s *communitySheet) mouse(m *Model, ev mouseEv, x, y int) tea.Cmd {
	if s.composing || s.busy {
		return nil
	}
	if ev == mouseWheelUp {
		return s.key(m, tea.KeyPressMsg{}, "up")
	}
	if ev == mouseWheelDown {
		return s.key(m, tea.KeyPressMsg{}, "down")
	}
	if ev == mousePress {
		if id := s.rowIDs[y]; id != "" {
			s.selected = id
			s.scroll = 0
		}
	}
	return nil
}
