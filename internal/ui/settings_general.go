package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/menubar"
)

// General is everything that holds whatever agent runs: how rush looks
// after sessions, then how it looks (interfaceSections).
func (m *Model) generalSections() []section {
	c := &m.store.Config
	d := &c.Dispatch
	var secs []section

	rest := choiceSetting("Rest idle sessions after", restValue(d.RestMinutes),
		"How long an idle rush-mode session keeps its agent running. An idle agent holds 150-580 MB; after this it stops, and your next message starts it again in about a second. The host, the conversation and its queue stay, and so does the prompt cache.",
		[][2]string{
			{"", "the agent stops a moment after its turn, with nothing left running in the background."},
			{"2 min", "the agent stays up 2 minutes after its turn."},
			{"5 min", "the agent stays up 5 minutes after its turn."},
			{"10 min", "the agent stays up 10 minutes after its turn."},
			{"30 min", "the agent stays up half an hour after its turn."},
		}, func(v string) {
			d.RestMinutes = 0
			fmt.Sscanf(v, "%d", &d.RestMinutes)
		})
	rest.unset = "at once"

	hib := "off"
	if c.Hibernate.AfterMinutes > 0 {
		hib = fmt.Sprintf("%dm", c.Hibernate.AfterMinutes)
	}
	hibernate := choiceSetting("Hibernate finished agents", hib,
		"A finished agent's process stays in memory (often 300–800 MB) until it is stopped. Hibernating stops it; the conversation is kept and enter resumes it.",
		[][2]string{
			{"off", "finished agents stay in memory until you stop them (ctrl+x)."},
			{"15m", "one that finished and sat idle 15 minutes is stopped."},
			{"30m", "one that finished and sat idle 30 minutes is stopped."},
			{"60m", "one that finished and sat idle an hour is stopped."},
		}, func(v string) {
			c.Hibernate.AfterMinutes = 0
			fmt.Sscanf(v, "%dm", &c.Hibernate.AfterMinutes)
		})

	cleanup := choiceSetting("Clean up done work after", cleanupValue(c.CleanupHours),
		"What happens to an agent's worktree and temp work once you've marked it done (alt+d) and left it alone. Stopping an agent never removes anything. A worktree goes only if git says every change in it is committed and pushed; its branch stays. One that isn't is kept, and Agents › Projects says why on the worktree.",
		[][2]string{
			{"", "done work that's committed and pushed goes 3 hours after it was last touched."},
			{"1h", "done work that's committed and pushed goes an hour after it was last touched."},
			{"12h", "done work that's committed and pushed goes 12 hours after it was last touched."},
			{"24h", "done work that's committed and pushed goes a day after it was last touched."},
			{"off", "nothing goes by itself; x on a worktree in Agents › Projects removes it."},
		}, func(v string) {
			switch v {
			case "off":
				c.CleanupHours = -1
			case "":
				c.CleanupHours = 0
			default:
				fmt.Sscanf(v, "%dh", &c.CleanupHours)
			}
		})
	cleanup.unset = "3h"

	secs = append(secs, section{title: "Idle and finished", rows: []setting{rest, hibernate, cleanup}})

	notify := choiceSetting("Notify when an agent needs you", onOffWord(!c.Quiet),
		"A macOS notification when an agent starts waiting on you (a question or a permission), not when you have already seen it.",
		[][2]string{{"on", "you are notified once per new question."}, {"off", "the Needs you section is the only signal."}},
		func(v string) { c.Quiet = v == "off" })
	menu := choiceSetting("Menu bar icon", onOffWord(c.MenuBar),
		"rush in the menu bar: every account's usage, what's working, and who needs you, with a badge. A question with a few answers can be answered from its notification's buttons, a permission allowed or denied. The first time, it's built with Xcode's Swift compiler (a few seconds).",
		[][2]string{{"on", "it opens with rush, and its menu can open it at login; rush's own notifications give way to its."}, {"off", "no menu bar icon."}}, nil)
	menu.run = func(v string) tea.Cmd {
		c.MenuBar, c.MenuBarAsked = v == "on", true
		if !c.MenuBar {
			// It reads its lock file and runs pkill: off the UI.
			return func() tea.Msg { menubar.Stop(); return nil }
		}
		return m.startMenuBar()
	}
	secs = append(secs, section{title: "When an agent needs you", rows: []setting{notify, menu}})

	advisor := choiceSetting("Advisor", onOffWord(c.Advisor),
		"Now and then, after your agents have done some work, rush's advisor looks over their figures on Haiku, with Opus checking what it finds, and says what would cut tokens or time. Its notes are in Efficiency. It costs a little each pass.",
		[][2]string{{"on", "it looks at most every 3 hours, once there's something new."}, {"off", "no advisor."}}, nil)
	advisor.run = m.advCommand
	secs = append(secs, section{title: "Advice", rows: []setting{advisor}})
	return append(secs, m.interfaceSections()...)
}

// onOffWord is on or off.
func onOffWord(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func restValue(min int) string {
	if min <= 0 {
		return ""
	}
	return fmt.Sprintf("%d min", min)
}

func cleanupValue(h int) string {
	switch {
	case h < 0:
		return "off"
	case h > 0:
		return fmt.Sprintf("%dh", h)
	}
	return ""
}
