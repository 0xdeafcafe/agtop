package ui

import (
	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/agent/event"
)

// commandNeeds are agtop's session commands only some agents can do, and
// the feature each needs.
var commandNeeds = map[string]agent.Feature{
	"fork": agent.FeatureFork, "rewind": agent.FeatureRewind, "effort": agent.FeatureEffort, "plan": agent.FeaturePlan,
	"tasks": agent.FeatureBackground, "subtask": agent.FeatureSubagents,
	"btw": agent.FeatureSideQuestion, "cd": agent.FeatureDirs, "add-dir": agent.FeatureDirs,
}

// sessionAgent is the agent a session runs.
func sessionAgent(c *hostConn) agent.Kind { return agent.KindOf(c.sess.Info.Kind) }

// canRun is whether the session's agent can do the command agtop would
// run for name.
func canRun(c *hostConn, name string) bool {
	need, gated := commandNeeds[name]
	return !gated || agent.Supports(sessionAgent(c), need)
}

// ownScreens is whether the session's agent has screens of its own, which
// agtop shows or hands the terminal to.
func ownScreens(c *hostConn) bool { return agent.Supports(sessionAgent(c), agent.FeatureScreen) }

// sessionCommands are agtop's commands this session's agent can do.
func sessionCommands(c *hostConn) []event.Command {
	var out []event.Command
	for _, cmd := range agtopCommands {
		if canRun(c, cmd.Name) {
			out = append(out, cmd)
		}
	}
	return out
}
