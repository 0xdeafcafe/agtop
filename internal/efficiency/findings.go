package efficiency

import (
	"fmt"
	"path/filepath"
	"sort"
	"time"
)

// Finding is something in the figures worth doing something about, with
// the saver that addresses it when there is one.
type Finding struct {
	Title  string
	Detail string
	// Cost is roughly the dollars involved in the view, when it can be
	// worked out; findings with one come first, biggest first.
	Cost float64
	// Fix is the saver that addresses it: enter sets it up.
	Fix  string
	rank int // lower first among findings without a cost; below 0 goes first of all
}

// Findings are the view's findings, most at stake first.
func Findings(v *View, found map[string]Found) []Finding {
	var out []Finding
	add := func(f Finding) { out = append(out, f) }
	status := func(id string) Status { return found[id].Status }
	cost := v.Total.Cost()
	if v.Total.Req == 0 {
		return nil
	}

	for i := range Catalog {
		s := &Catalog[i]
		if f := found[s.ID]; f.Status == Partial {
			add(Finding{Title: fmt.Sprintf("%s is half set up: %s", s.Name, f.Wants), Detail: "it does nothing until it's finished", Fix: s.ID, rank: -1})
		}
	}

	all, share := v.ToolBytes()
	if share[ToolBash] > 0.3 && all > 1<<20 {
		f := Finding{
			Title:  fmt.Sprintf("Shell output is %.0f%% of what tools sent back (%s)", share[ToolBash]*100, Bytes(v.Total.Bytes[ToolBash])),
			Detail: fmt.Sprintf("every token of it is read again by the requests after it: carrying it cost about %s", Money(v.Carry[ToolBash])),
			Cost:   v.Carry[ToolBash],
		}
		switch {
		case status("bash-output") == Off:
			f.Fix = "bash-output"
		case status("rtk") != On:
			f.Fix = "rtk"
		}
		add(f)
	}
	if share[ToolRead] > 0.25 && all > 1<<20 {
		f := Finding{
			Title:  fmt.Sprintf("File reads are %.0f%% of what tools sent back (%s)", share[ToolRead]*100, Bytes(v.Total.Bytes[ToolRead])),
			Detail: fmt.Sprintf("carrying them cost about %s", Money(v.Carry[ToolRead])),
			Cost:   v.Carry[ToolRead],
		}
		if v.Rereads > 0 {
			f.Detail += fmt.Sprintf("; %d sessions read one file %d+ times (most: %s, %d times)", v.Rereads, RereadAt, filepath.Base(v.WorstRead), v.WorstReads)
		}
		add(f)
	}
	if v.StartMedian > 30_000 {
		carry := float64(v.StartMedian) * float64(v.Total.Req) * cacheReadPrice("") / 1e6
		add(Finding{
			Title:  fmt.Sprintf("Sessions start with %s tokens of context (p90 %s)", Tokens(v.StartMedian), Tokens(v.StartP90)),
			Detail: fmt.Sprintf("the system prompt, CLAUDE.md, skills, plugins and MCP tools, read again by every request: about %s here. /context in a session shows what's in it", Money(carry)),
			Cost:   carry,
		})
	}
	if v.Fat > 0 {
		f := Finding{
			Title:  fmt.Sprintf("%d sessions went past %s tokens of context", v.Fat, Tokens(FatContext)),
			Detail: "every request re-reads all of it; compacting sooner keeps each turn cheaper",
			rank:   1,
		}
		if status("autocompact") == Off {
			f.Fix = "autocompact"
		}
		add(f)
	}
	if v.SubCost > 5 && v.SubBigCost > 0.3*v.SubCost && status("subagent-model") == Off {
		add(Finding{
			Title:  fmt.Sprintf("Subagents cost %s, %s of it on Opus or Fable", Money(v.SubCost), Money(v.SubBigCost)),
			Detail: "most subagent work is searching and reading; on Sonnet it would cost about half",
			Cost:   v.SubBigCost / 2,
			Fix:    "subagent-model",
		})
	}
	if v.Big > 10 && status("bash-output") == Off {
		add(Finding{
			Title:  fmt.Sprintf("%d tool results were over %s tokens each", v.Big, Tokens(BigResult/4)),
			Detail: "a lower cap keeps the start and end and saves the rest to a file",
			Fix:    "bash-output",
			rank:   2,
		})
	}
	if hit := v.CacheHit(); hit < 90 && v.Total.Req > 200 {
		add(Finding{
			Title:  fmt.Sprintf("Only %.0f%% of input came from the cache", hit),
			Detail: "sessions left idle past the cache's life, or switching model, write the whole context again",
			rank:   2,
		})
	}
	if cost > 0 {
		if out := v.Total.COut / cost; out < 0.12 {
			add(Finding{
				Title:  fmt.Sprintf("What Claude writes is only %.0f%% of the spend", out*100),
				Detail: "savers that shorten replies can save at most that; context is where the money goes",
				rank:   3,
			})
		}
	}
	if v.Total.Out > 0 && float64(v.Total.Think)/float64(v.Total.Out) > 0.7 && v.Total.COut > 0.15*cost {
		add(Finding{
			Title:  fmt.Sprintf("Thinking is %.0f%% of output", 100*float64(v.Total.Think)/float64(v.Total.Out)),
			Detail: "a lower effort (/effort) thinks less on routine work",
			rank:   3,
		})
	}
	for i := range Catalog {
		s := &Catalog[i]
		if found[s.ID].Status == On && len(s.Uses) > 0 && v.Uses[s.ID] == nil && v.Q.To.Sub(v.Q.From) >= 72*time.Hour {
			add(Finding{Title: s.Name + " is on but wasn't seen in these sessions", Detail: "it may not be loading, or not apply to this work", rank: 3})
		}
	}
	if status("history") == Off {
		add(Finding{Title: "Claude Code deletes transcripts after 30 days", Detail: "agtop keeps totals of deleted ones, but not their detail", Fix: "history", rank: 4})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.rank < 0 || b.rank < 0 {
			return a.rank < b.rank
		}
		if (a.Cost > 0) != (b.Cost > 0) {
			return a.Cost > 0
		}
		if a.Cost != b.Cost {
			return a.Cost > b.Cost
		}
		return a.rank < b.rank
	})
	return out
}

// Money, Tokens and Bytes are figures in words, short.
func Money(c float64) string {
	switch {
	case c >= 10_000:
		return fmt.Sprintf("$%.1fk", c/1000)
	case c >= 100:
		return fmt.Sprintf("$%.0f", c)
	case c >= 1:
		return fmt.Sprintf("$%.2f", c)
	}
	return fmt.Sprintf("$%.3f", c)
}

func Tokens(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 10_000:
		return fmt.Sprintf("%.0fk", float64(n)/1e3)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprintf("%d", n)
}

func Bytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
