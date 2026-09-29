package ui

import (
	"github.com/0xdeafcafe/agtop/internal/convo"
)

// interfaceSections are how agtop looks and what its keys do.
func (m *Model) interfaceSections() []section {
	c := &m.store.Config

	view := choiceSetting("Layout", c.View,
		"How agtop lays out Agents and the Session, now and next time. #view and shift+← → change it too.",
		[][2]string{
			{"split", "Agents on the left, the picked agent's Session beside them, when the screen is wide enough."},
			{"agent", "one Session with the whole screen; esc shows Agents, and the next one you open has the whole screen too."},
			{"list", "Agents alone; a Session you open takes the screen until esc."},
			{"", "agtop asks which the next time it opens."},
		}, func(v string) {
			if v == "" {
				c.View = ""
				return
			}
			c.SetView(v)
		})
	view.unset = "ask"
	enter := choiceSetting("Enter on an agent", c.EnterOn,
		"What enter does on an agent in the list.",
		[][2]string{
			{"rename", "renames it, as in the Finder; ctrl+o opens it."},
			{"open", "opens its Session; ctrl+r renames it."},
			{"", "agtop asks the first time you press it."},
		}, func(v string) { c.EnterOn = v })
	enter.unset = "ask"
	group := choiceSetting("Group by", firstNonEmpty(c.GroupBy, "status"), "How agents are sorted into sections.", [][2]string{
		{"status", "Needs you, Working, Waiting on you and Idle first; finished agents from the last day under Today, older ones under Earlier."},
		{"agent", "one section per coding agent (Claude Code, Codex, Copilot…), handy when you run several."},
		{"group", "your own sections; put an agent in one with /group <name>. Ungrouped agents fall back to status."},
	}, func(v string) { c.GroupBy = v })
	group.choices = m.groupModes() // plugins arrange the list too
	split := choiceSetting("Split by project", map[bool]string{true: "on", false: "off"}[m.splitProjects()],
		"Whether each section's agents sit together by the repository they work in, whatever the grouping. ctrl+p, or the toggle at the top of the list, changes it too.",
		[][2]string{
			{"on", "under each section, a line per project with its branch, commits ahead and behind and uncommitted changes; agents in a linked worktree under the repository it came from."},
			{"off", "one run of rows per section, as the sort puts them."},
		}, func(v string) {
			c.SplitBy = map[bool]string{true: "project", false: "none"}[v == "on"]
			m.rebuild()
		})
	sortBy := choiceSetting("Sort rows by", firstNonEmpty(c.SortBy, "name"),
		"The order of rows inside each section. You can also click a column header on the Agents view.",
		[][2]string{
			{"name", "alphabetical, so a row stays put while its agent works."},
			{"recent", "most recently active first; rows move as agents update."},
			{"cost", "most expensive first."},
			{"cpu", "busiest first, by CPU across everything the agent started."},
			{"ram", "heaviest first, by memory across everything the agent started."},
			{"time", "longest-running first."},
		}, func(v string) { c.SortBy = v })

	theme := choiceSetting("Theme", c.Theme,
		"What agtop's colours are made for. Its text and panels are shades between your terminal's background and text colour, so they follow its theme; its orange, green and red stay, made as easy to read on your background as on agtop's own.",
		[][2]string{
			{"", "agtop asks the terminal for its background and text, again whenever you come back to it; a terminal that doesn't say gets agtop's dark."},
			{"dark", "agtop's own dark colours, whatever the terminal says."},
			{"light", "agtop's own light colours, for a light terminal that doesn't say it's light."},
		}, func(v string) {
			c.Theme = v
			m.applyColors()
		})
	theme.unset = "match terminal"
	colours := choiceSetting("Colours", map[bool]string{true: "colour-blind", false: "standard"}[c.ColorBlind],
		"How agtop tells good from bad: added and removed lines in a diff, done and failed steps and agents.",
		[][2]string{
			{"standard", "green and red."},
			{"colour-blind", "sky blue and amber, which stay apart for red-green and blue-yellow colour blindness alike; + and − still mark every diff line."},
		}, func(v string) {
			c.ColorBlind = v == "colour-blind"
			m.colored = false
			m.applyColors()
		})
	spaces := choiceSetting("Spaces and tabs in diffs", map[bool]string{true: "shown", false: "hidden"}[c.ShowWhitespace],
		"Whether a diff marks its spaces and tabs, as · and →, so a change in indentation shows.",
		[][2]string{{"hidden", "diffs show text only."}, {"shown", "every space is a faint · and every tab a →."}},
		func(v string) {
			c.ShowWhitespace = v == "shown"
			convo.SetShowWhitespace(c.ShowWhitespace)
		})

	search := choiceSetting("ctrl+k searches transcripts", map[bool]string{true: "on ctrl+enter", false: "as you type"}[c.SearchTranscriptsOnKey],
		"What the command bar (ctrl+k) looks through as you type.",
		[][2]string{
			{"as you type", "agent names, commands and every transcript, as you type."},
			{"on ctrl+enter", "names and commands as you type; ctrl+enter (or ctrl+j) then searches the transcripts."},
		}, func(v string) { c.SearchTranscriptsOnKey = v == "on ctrl+enter" })

	return []section{
		{title: "Look", rows: []setting{view, theme, colours, spaces}},
		{title: "Agents list", rows: []setting{group, split, sortBy, enter, search}},
	}
}
