package agent

import "time"

// Stats is an agent's own record of a profile's use over time: activity
// and tokens by day, and totals by model.
type Stats struct {
	Computed    string // the last day it counted, 2006-01-02
	First       time.Time
	Sessions    int
	Messages    int
	Days        []StatsDay // oldest first
	Models      []ModelTotal
	Hours       [24]int // sessions started in each hour of the day
	Longest     time.Duration
	LongestMsgs int
	LongestID   string
	LongestAt   time.Time
}

// StatsDay is one day's activity.
type StatsDay struct {
	Date                          time.Time
	Messages, Sessions, ToolCalls int
	Tokens                        map[string]int64 // by model
}

// TotalTokens is the day's tokens across models.
func (d StatsDay) TotalTokens() int64 {
	var n int64
	for _, t := range d.Tokens {
		n += t
	}
	return n
}

// ModelTotal is one model's tokens, all time.
type ModelTotal struct {
	Model                          string
	In, Out, CacheRead, CacheWrite int64
	CostUSD                        float64
}

// Total is every token the model read or wrote.
func (t ModelTotal) Total() int64 { return t.In + t.Out + t.CacheRead + t.CacheWrite }

// StatsReader is an agent that keeps a record of a profile's use.
type StatsReader interface {
	Stats(p Profile) (Stats, error)
}
