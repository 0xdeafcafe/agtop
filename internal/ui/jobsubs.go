package ui

import (
	"slices"
	"strings"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/convo"
)

// An agent that keeps no transcript for a subagent of its own (ACP's, or a
// Codex thread not found on disk yet) still says when one starts and ends,
// as a task: it's a subagent row from that alone, its state the task's.
// Claude Code's runs all have transcripts, so its sessions list none.

// jobSubPrefix marks a subagent known only by its task.
const jobSubPrefix = "job:"

// jobSubs are the subagent tasks have lists no row for yet.
func (c *hostConn) jobSubs(have []convo.Subagent) []convo.Subagent {
	if c.sess == nil || agent.ReadsAsClaude(sessionAgent(c)) {
		return nil
	}
	var out []convo.Subagent
	for _, j := range c.sess.Jobs() {
		if c.jobKind(j) != "subagent" || c.spawnJob(j) {
			continue
		}
		if j.ToolUseID != "" && slices.ContainsFunc(have, func(sa convo.Subagent) bool { return sa.ToolUseID == j.ToolUseID }) {
			continue
		}
		out = append(out, convo.Subagent{ID: jobSubPrefix + j.ID, Type: firstNonEmpty(j.Agent, "subagent"), Description: j.Label,
			ToolUseID: j.ToolUseID, Depth: 1, Born: j.Start.UnixNano()})
	}
	return out
}

// withJobSubs is subs with the subagent tasks they lack.
func (c *hostConn) withJobSubs(subs []convo.Subagent) []convo.Subagent {
	subs = slices.DeleteFunc(subs, func(sa convo.Subagent) bool { return strings.HasPrefix(sa.ID, jobSubPrefix) })
	return append(subs, c.jobSubs(subs)...)
}

// jobSubState is how a subagent known by its task stands: as the task.
func (c *hostConn) jobSubState(sa convo.Subagent) (status string, live bool) {
	id := strings.TrimPrefix(sa.ID, jobSubPrefix)
	for _, j := range c.sess.Jobs() {
		if j.ID == id {
			if j.Running() {
				return "", true
			}
			return j.Status, false
		}
	}
	return "", false
}
