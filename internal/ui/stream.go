package ui

import (
	"slices"
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
	streamKeep      = 40
	streamFresh     = 8 * time.Second // a new post stands out this long
)

type streamPost struct {
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

// streamOf is the board's posts oldest first, the last streamKeep of them.
func streamOf(threads []community.Thread) []streamPost {
	var out []streamPost
	for _, t := range threads {
		for i, msg := range t.Messages {
			out = append(out, streamPost{id: t.ID, title: t.Title, author: msg.Author, text: msg.Text, at: msg.At, reply: i > 0})
		}
	}
	slices.SortStableFunc(out, func(a, b streamPost) int { return a.at.Compare(b.at) })
	if len(out) > streamKeep {
		out = out[len(out)-streamKeep:]
	}
	return out
}

// streamLines is the timeline in at most room rows, newest at the bottom,
// whole posts only; nil when not even a heading and one post fit.
func (m *Model) streamLines(w, room int) (lines, keys []string) {
	if room < 4 || len(m.stream.posts) == 0 {
		return nil, nil
	}
	now := time.Now()
	var body, bodyKeys []string
	for i := len(m.stream.posts) - 1; i >= 0; i-- {
		post := m.streamPost(m.stream.posts[i], w, now)
		if len(body)+len(post) > room-2 {
			break
		}
		key := streamKeyPrefix + m.stream.posts[i].id
		body = append(post, body...)
		bodyKeys = append(slices.Repeat([]string{key}, len(post)), bodyKeys...)
	}
	if body == nil {
		return nil, nil
	}
	return append([]string{"", faint("  community")}, body...), append([]string{"", ""}, bodyKeys...)
}

// streamPost is one post as a tweet: who and when, what thread a reply is
// in, then up to three lines of what they said.
func (m *Model) streamPost(p streamPost, w int, now time.Time) []string {
	when := dim(age(now.Sub(p.at)))
	name := paint(cText, "@"+communityText(p.author.Username()))
	if now.Sub(p.at) < streamFresh {
		name = paint(cOrange, "● ") + name
	}
	head := "  " + name
	if p.reply {
		head += dim(" ↩ " + communityText(p.title))
	}
	out := []string{spread(fit(head, w-cellw.String(when)-1), when, w)}
	text := communityText(p.text)
	if !p.reply {
		text = communityText(p.title)
	}
	wrapped := wrap(strings.Join(strings.Fields(text), " "), max(8, w-5))
	if len(wrapped) > 3 {
		wrapped = append(wrapped[:2], cellw.Truncate(wrapped[2], max(8, w-6), "")+"…")
	}
	for _, l := range wrapped {
		out = append(out, "    "+dim(l))
	}
	return out
}
