package ui

import (
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/0xdeafcafe/rush/internal/cellw"
	"github.com/0xdeafcafe/rush/internal/community"
)

// The stream is the community board as a timeline in the list's empty
// foot: every question and reply, newest at the bottom, the older ones
// pushed up and off the top as posts come in. A click opens the thread.

const (
	streamKeyPrefix = "community:"
	streamFresh     = 8 * time.Second // a new post stands out this long
)

type streamPost struct {
	key       string // the post's own id: thread id, "/", its place in the thread
	id, title string
	author    community.Author
	text      string
	at        time.Time
	reply     bool
}

type streamState struct {
	posts []streamPost
	stamp string
}

// streamTick reads the board again in 2s, if it changed.
func (m *Model) streamTick() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return sheetMsg{apply: func(m *Model) tea.Cmd { return m.loadStream() }}
	})
}

func (m *Model) loadStream() tea.Cmd {
	if !m.store.Config.Twatter {
		return m.streamTick()
	}
	stamp := m.stream.stamp
	return sheetDo(func() ([]streamPost, error) {
		token, err := community.Version()
		if err != nil || token == stamp {
			return nil, err
		}
		threads, err := community.List()
		if err != nil {
			return nil, err
		}
		stamp = token
		return streamOf(threads), nil
	}, func(m *Model, posts []streamPost, err error) tea.Cmd {
		if err == nil && posts != nil {
			m.stream = streamState{posts, stamp}
		}
		return m.streamTick()
	})
}

// streamOf is the whole board's posts, oldest first.
func streamOf(threads []community.Thread) []streamPost {
	var out []streamPost
	for _, t := range threads {
		for i, msg := range t.Messages {
			out = append(out, streamPost{key: t.ID + "/" + strconv.Itoa(i), id: t.ID, title: t.Title, author: msg.Author, text: msg.Text, at: msg.At, reply: i > 0})
		}
	}
	slices.SortStableFunc(out, func(a, b streamPost) int { return a.at.Compare(b.at) })
	return out
}

// streamLines is the timeline in at most room rows, newest at the bottom,
// whole posts only; nil when not even a heading and one post fit.
func (m *Model) streamLines(w, room int) (lines, keys []string) {
	if !m.store.Config.Twatter || room < 5 || len(m.stream.posts) == 0 || w < 24 {
		return nil, nil
	}
	now := time.Now()
	inner := w - 5 // "│ " … " │", and a column clear of the divider
	var body, bodyKeys []string
	seen := map[string]bool{}
	for i := len(m.stream.posts) - 1; i >= 0 && len(body) < room-3; i-- {
		p := m.stream.posts[i]
		said := p.author.Username() + "\x00" + p.said()
		if seen[said] {
			continue // the same agent saying the same thing again
		}
		seen[said] = true
		body = append([]string{m.streamRow(p, inner, now)}, body...)
		bodyKeys = append([]string{streamKeyPrefix + p.key}, bodyKeys...)
	}
	if body == nil {
		return nil, nil
	}
	edge := func(s string) string { return paint(cSub, s) }
	title := " Twatter "
	lines = []string{"", edge("╭─") + paint(cText+bold, title) + edge(strings.Repeat("─", max(0, inner+1-cellw.String(title)))+"╮")}
	for _, l := range body {
		lines = append(lines, edge("│")+" "+fit(l, inner)+" "+edge("│"))
	}
	lines = append(lines, edge("╰"+strings.Repeat("─", inner+2)+"╯"))
	return lines, append(append([]string{"", streamKeyPrefix}, bodyKeys...), streamKeyPrefix)
}

// streamRow is one post on one line for the side: who, what, how long ago.
func (m *Model) streamRow(p streamPost, w int, now time.Time) string {
	when := dim(age(now.Sub(p.at)))
	if now.Sub(p.at) < streamFresh {
		when = paint(cOrange, age(now.Sub(p.at)))
	}
	left := paint(cText+bold, streamHandle(p)) + "  " + tagged(strings.Join(strings.Fields(communityText(p.said())), " "))
	return spread(fit(left, w-cellw.String(when)-2), when, w)
}

// streamHandle is the author as @name, whatever Username already carries.
func streamHandle(p streamPost) string {
	return "@" + strings.TrimPrefix(communityText(p.author.Username()), "@")
}

// said is what the post says: a reply's text, or a post's title.
func (p streamPost) said() string {
	if p.reply {
		return p.text
	}
	return p.title
}

// streamPost is one post as a tweet: who and when, what thread a reply is
// in, then up to three lines of what they said.
func (m *Model) streamPost(p streamPost, w int, now time.Time) []string {
	name := paint(cText+bold, streamHandle(p))
	if now.Sub(p.at) < streamFresh {
		name = paint(cOrange, "● ") + name
	}
	head := "  " + name + dim(" · "+age(now.Sub(p.at)))
	if p.reply {
		head += dim(" ↩ " + communityText(p.title))
	}
	out := []string{fit(head, w)}
	text := communityText(p.text)
	if !p.reply {
		text = communityText(p.title)
	}
	wrapped := wrap(strings.Join(strings.Fields(text), " "), max(8, w-5))
	if len(wrapped) > 3 {
		wrapped = append(wrapped[:2], cellw.Truncate(wrapped[2], max(8, w-6), "")+"…")
	}
	for _, l := range wrapped {
		out = append(out, "    "+tagged(l))
	}
	return out
}

// tagged paints a line's @mentions and #hashtags; the rest stays plain.
func tagged(line string) string {
	words := strings.Split(line, " ")
	for i, w := range words {
		switch {
		case len(w) > 1 && w[0] == '@':
			words[i] = paint(cBlue, w)
		case len(w) > 1 && w[0] == '#':
			words[i] = paint(cQueue, w)
		default:
			words[i] = paint(cSub, w)
		}
	}
	return strings.Join(words, " ")
}
