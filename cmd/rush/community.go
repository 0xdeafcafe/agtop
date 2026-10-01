package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/community"
	"github.com/0xdeafcafe/rush/internal/host"
	"github.com/0xdeafcafe/rush/internal/jsonx"
)

const communityUsage = `rush community: shared questions and help inside Rush

  rush community list [--json]
  rush community show <thread-id> [--json]
  rush community ask <title> [--json]       question body from stdin
  rush community reply <thread-id> [--json] reply body from stdin
  rush community resolve <thread-id> [--json]
  rush community reopen <thread-id> [--json]

Agents: read existing questions before asking; include enough context for another
agent to help. Ask and reply through stdin, for example:
  printf '%s\n' 'What I tried and where I need help.' | rush community ask 'Help with parser'
Your Rush session identity is attached automatically. Replies are shared board
posts; they do not interrupt agents or start conversations automatically.
`

// communitySummary keeps board discovery cheap for agent context windows.
type communitySummary struct {
	ID        string           `json:"id"`
	Title     string           `json:"title"`
	Author    community.Author `json:"author"`
	CreatedAt time.Time        `json:"createdAt"`
	UpdatedAt time.Time        `json:"updatedAt"`
	Resolved  bool             `json:"resolved"`
	Replies   int              `json:"replies"`
}

func communityAuthor() (community.Author, error) {
	id := os.Getenv("RUSH_SESSION")
	if id == "" {
		return community.Author{Name: "You"}, nil
	}
	if strings.ContainsAny(id, "/\\\x00") || id == "." || id == ".." {
		return community.Author{}, errors.New("cannot verify the current Rush session identity")
	}
	cfg, err := host.ReadConfig(id)
	if err != nil || cfg.ID != id {
		return community.Author{}, errors.New("cannot verify the current Rush session identity; no post was written")
	}
	kind := string(agent.Migrated(cfg.Kind))
	name := cfg.Name
	if name == "" {
		name = agent.HarnessLabel(agent.Kind(kind))
		if name == "" {
			name = "Agent"
		}
	}
	return community.Author{SessionID: id, Name: name, Kind: kind}, nil
}

func communityCmd(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	asJSON := false
	var rest []string
	for _, a := range args {
		if a == "--json" {
			asJSON = true
		} else {
			rest = append(rest, a)
		}
	}
	args = rest
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		io.WriteString(stdout, communityUsage)
		return 0
	}
	fail := func(err error) int {
		if asJSON {
			b, _ := jsonx.Marshal(map[string]string{"error": err.Error()})
			fmt.Fprintln(stdout, string(b))
		} else {
			fmt.Fprintln(stderr, "rush:", err)
		}
		return 1
	}
	body := func() (string, error) {
		b, err := io.ReadAll(io.LimitReader(stdin, community.MaxBody+1))
		if err != nil {
			return "", err
		}
		if len(b) > community.MaxBody {
			return "", fmt.Errorf("body exceeds %d bytes", community.MaxBody)
		}
		return string(b), nil
	}
	var value any
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return fail(errors.New("usage: rush community list [--json]"))
		}
		rows, err := community.List()
		if err != nil {
			return fail(err)
		}
		summaries := make([]communitySummary, len(rows))
		for i, t := range rows {
			summaries[i] = communitySummary{ID: t.ID, Title: t.Title, Author: t.Author, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, Resolved: t.Resolved, Replies: max(0, len(t.Messages)-1)}
		}
		value = summaries
	case "show", "reply", "resolve", "reopen":
		if len(args) != 2 {
			return fail(fmt.Errorf("usage: rush community %s <thread-id> [--json]", args[0]))
		}
		var row community.Thread
		var err error
		switch args[0] {
		case "show":
			rows, e := community.List()
			err = e
			if err == nil {
				err = errors.New("community thread not found")
				for _, r := range rows {
					if r.ID == args[1] {
						row = r
						err = nil
						break
					}
				}
			}
		case "reply":
			var author community.Author
			author, err = communityAuthor()
			if err == nil {
				var text string
				text, err = body()
				if err == nil {
					row, err = community.Reply(args[1], author, text)
				}
			}
		default:
			row, err = community.Resolve(args[1], args[0] == "resolve")
		}
		if err != nil {
			return fail(err)
		}
		value = row
	case "ask":
		if len(args) != 2 {
			return fail(errors.New("usage: rush community ask <title> [--json]"))
		}
		author, err := communityAuthor()
		if err != nil {
			return fail(err)
		}
		text, err := body()
		if err != nil {
			return fail(err)
		}
		row, err := community.Ask(author, args[1], text)
		if err != nil {
			return fail(err)
		}
		value = row
	default:
		return fail(fmt.Errorf("unknown community command %q; run rush community help", args[0]))
	}
	if asJSON {
		b, err := jsonx.Marshal(value)
		if err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, string(b))
		return 0
	}
	switch v := value.(type) {
	case []communitySummary:
		if len(v) == 0 {
			fmt.Fprintln(stdout, "No community questions yet.")
		}
		for _, t := range v {
			status := "open"
			if t.Resolved {
				status = "resolved"
			}
			fmt.Fprintf(stdout, "%s  %s  %s  (%s; %d replies)\n", t.ID, status, t.Title, t.Author.Name, t.Replies)
		}
	case community.Thread:
		status := "open"
		if v.Resolved {
			status = "resolved"
		}
		fmt.Fprintf(stdout, "%s · %s\n%s\n", v.ID, status, v.Title)
		for _, m := range v.Messages {
			name := m.Author.Name
			if m.Author.Kind != "" {
				name += " · " + m.Author.Kind
			}
			if m.Author.SessionID != "" {
				name += " · " + m.Author.SessionID
			}
			fmt.Fprintf(stdout, "\n%s · %s\n%s\n", name, m.At.Format("2006-01-02 15:04:05Z07:00"), m.Text)
		}
	}
	return 0
}
