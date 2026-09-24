package fleet

import "testing"

func TestOrphanCmd(t *testing.T) {
	cases := map[string]string{
		`/bin/zsh -c source /u/.claude/shell-snapshots/s.sh 2>/dev/null || true && eval 'haven up --agent 2>&1 | tail -8' < /dev/null && pwd -P >| /tmp/claude-781c-cwd`: `haven up --agent 2>&1 | tail -8`,
		`/bin/zsh -c source x && eval 'curl -w '"'"'%{http_code}'"'"' u; echo ok' < /dev/null`:                                                                           `curl -w '%{http_code}' u; echo ok`,
		`/Users/lw/.claude/bin/thing --flag`: `/Users/lw/.claude/bin/thing --flag`,
	}
	for in, want := range cases {
		if got := ShellCmd(in); got != want {
			t.Errorf("ShellCmd(%q) = %q, want %q", in, got, want)
		}
	}
}
