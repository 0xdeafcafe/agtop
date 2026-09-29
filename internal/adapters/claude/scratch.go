package claude

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/0xdeafcafe/rush/internal/adapters/claude/claude"
	"github.com/0xdeafcafe/rush/internal/agent"
)

// Scratch is a background job's own tmp folder, and every session's
// scratch (task output and the like) under /tmp/claude-<uid>.
func (a Adapter) Scratch(p agent.Profile, job, sid, cwd string) []agent.TempDir {
	var out []agent.TempDir
	if job != "" {
		out = append(out, agent.TempDir{Path: filepath.Join(Account(p).JobsDir(), job, "tmp"), Keep: true})
	}
	if sid != "" && cwd != "" {
		out = append(out, agent.TempDir{Path: filepath.Join(a.ScratchRoot(), claude.ProjectSlug(cwd), sid)})
	}
	return out
}

// ScratchRoot is where Claude Code keeps per-session scratch.
func (Adapter) ScratchRoot() string {
	return filepath.Join("/tmp", fmt.Sprintf("claude-%d", os.Getuid()))
}

var _ agent.Scratcher = Adapter{}
