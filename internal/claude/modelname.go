package claude

import (
	"regexp"
	"strings"
)

var modelParen = regexp.MustCompile(`\s*\(.*\)$`)

var modelID = regexp.MustCompile(`^claude-([a-z]+)-(\d+)(?:-(\d{1,2}))?(?:-\d{8})?(\[1m\])?$`)

// ModelName is how a model is shown: "Opus 5.5" for claude-opus-5-5[1m],
// a display name without its "(1M context)".
func ModelName(s string) string {
	if m := modelID.FindStringSubmatch(s); m != nil {
		v := m[2]
		if m[3] != "" {
			v += "." + m[3]
		}
		return strings.ToUpper(m[1][:1]) + m[1][1:] + " " + v
	}
	s = modelParen.ReplaceAllString(s, "")
	if s != "" && strings.ToLower(s) == s && !strings.Contains(s, "-") {
		return strings.ToUpper(s[:1]) + s[1:] // opus → Opus
	}
	return s
}
