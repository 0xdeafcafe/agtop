package ui

import (
	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/headless"
)

// commandNeeds are agtop's session commands only some agents can do.
var commandNeeds = map[string]agent.Caps{
	"fork": agent.CapFork, "rewind": agent.CapRewind, "effort": agent.CapEffort, "plan": agent.CapModes,
	"tasks": agent.CapBackground, "subtask": agent.CapSubagents,
}

// claudeOnly are agtop's session commands that work through Claude Code
// itself: its flags, its transcripts or its side questions.
var claudeOnly = map[string]bool{"btw": true, "cd": true, "add-dir": true}

// otherAgent is the agent a session runs when it isn't Claude Code.
func otherAgent(c *hostConn) (agent.Adapter, bool) {
	kind := c.sess.Info.Kind
	if kind == "" || kind == "claude" {
		return nil, false
	}
	a, ok := agent.Get(agent.Kind(kind))
	return a, ok
}

// canRun is whether the session's agent can do the command agtop would
// run for name; Claude Code can do them all.
func canRun(c *hostConn, name string) bool {
	a, ok := otherAgent(c)
	if !ok {
		return true
	}
	if claudeOnly[name] {
		return false
	}
	need, gated := commandNeeds[name]
	return !gated || a.Caps().Has(need)
}

// sessionCommands are agtop's commands this session's agent can do.
func sessionCommands(c *hostConn) []headless.Command {
	if _, ok := otherAgent(c); !ok {
		return agtopCommands
	}
	var out []headless.Command
	for _, cmd := range agtopCommands {
		if canRun(c, cmd.Name) {
			out = append(out, cmd)
		}
	}
	return out
}
