package harnessup

import (
	"regexp"
	"strconv"
	"strings"
)

// versionRe is the first version-looking token of a program's --version:
// "2.1.284 (Claude Code)", "codex-cli 0.155.1", "v1.0.3-beta.2".
var versionRe = regexp.MustCompile(`v?(\d+\.\d+(?:\.\d+)*(?:-[0-9A-Za-z][0-9A-Za-z.]*)?)`)

// Parse is the version in out, or empty when none looks like one.
func Parse(out string) string {
	if m := versionRe.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

// Newer is whether latest is a later version than installed. Numbers go
// by value (0.9 is before 0.10); at equal numbers a pre-release
// ("-beta") is before the release. Either empty is never newer.
func Newer(latest, installed string) bool {
	if latest == "" || installed == "" {
		return false
	}
	lv, lpre, _ := strings.Cut(latest, "-")
	iv, ipre, _ := strings.Cut(installed, "-")
	ln, in := numbers(lv), numbers(iv)
	for i := 0; i < max(len(ln), len(in)); i++ {
		var l, c int
		if i < len(ln) {
			l = ln[i]
		}
		if i < len(in) {
			c = in[i]
		}
		if l != c {
			return l > c
		}
	}
	return ipre != "" && lpre == ""
}

func numbers(v string) []int {
	var out []int
	for _, p := range strings.Split(v, ".") {
		n, _ := strconv.Atoi(p)
		out = append(out, n)
	}
	return out
}
