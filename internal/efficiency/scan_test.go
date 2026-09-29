package efficiency

import (
	"testing"
)

func TestMatches(t *testing.T) {
	rtk, cave, serena := Find("rtk"), Find("caveman"), Find("serena")
	cases := []struct {
		s    *Saver
		key  string
		want bool
	}{
		{rtk, "bash:rtk", true},
		{rtk, "bash:rtk/admin", false},
		{rtk, "hook:/opt/homebrew/bin/rtk hook claude", true},
		{rtk, "hook:node other.js", false},
		{cave, "skill:caveman:caveman", true},
		{cave, "skill:caveman", true},
		{cave, "cmd:/caveman", true},
		{cave, "cmd:/cavemanx", false},
		{cave, "hook:node x/caveman-activate.js", true},
		{serena, "mcp:serena", true},
		{serena, "mcp:serenade", false},
		{Find("context-mode"), "mcp:plugin_context-mode_context-mode", true},
	}
	for _, c := range cases {
		if got := c.s.Matches(c.key); got != c.want {
			t.Errorf("%s matches %q = %v, want %v", c.s.ID, c.key, got, c.want)
		}
	}
}

func TestFirstWord(t *testing.T) {
	for cmd, want := range map[string]string{
		"rtk git status":              "rtk",
		"FOO=1 BAR=2 /usr/bin/rtk ls": "rtk",
		"'rtk' ls":                    "rtk",
		"":                            "",
	} {
		if got, _ := firstWord(cmd); got != want {
			t.Errorf("firstWord(%q) = %q, want %q", cmd, got, want)
		}
	}
	if _, admin := firstWord("rtk --version"); !admin {
		t.Error("rtk --version is looking after rtk, not using it")
	}
}

func TestLooks(t *testing.T) {
	for cmd, want := range map[string]bool{
		`rg -n foo internal`:                      true,
		`cd /x && grep -rn foo . | head -20`:      true,
		`rtk grep foo`:                            true,
		`sed -n 10,40p a.go`:                      true,
		`sed -i s/a/b/ a.go`:                      false,
		`git grep foo`:                            true,
		`git status`:                              false,
		`find . -name '*.go' 2>/dev/null || true`: true,
		`go test ./... && echo done`:              false,
		`LC_ALL=C cat a.go`:                       true,
		"cd /x\nls -la":                           true,
		`python3 - <<'EOF'`:                       false,
		`cat > a.go <<'EOF'`:                      false,
	} {
		if got := Looks(cmd); got != want {
			t.Errorf("Looks(%q) = %v, want %v", cmd, got, want)
		}
	}
}
