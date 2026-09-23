package ui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// screenInfo is what a Claude Code agent's own screen shows that its
// transcript doesn't: the working line (with elapsed time and tokens), the
// todo list under it, and the status lines under the prompt.
type screenInfo struct {
	working string
	todos   []string
	status  []string
}

var (
	spinLine = regexp.MustCompile(`^\s*[·✢✳✶✻✽*]\s+\S.*…`)
	todoLine = regexp.MustCompile(`[☐☒◼■□✓✔]`)
	ruleLine = regexp.MustCompile(`^\s*[─━╭╰]{3,}`)
)

// readScreen picks those out of the screen's lines. Claude Code draws the
// prompt between two rules near the bottom; the working line and todos sit
// above it, the status lines below.
func readScreen(lines []string) screenInfo {
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = strings.TrimRight(ansi.Strip(l), " ")
	}
	// The prompt: the last line starting with ❯ or > between rules.
	prompt := -1
	for i := len(plain) - 1; i > 0; i-- {
		t := strings.TrimLeft(plain[i], " │")
		if (strings.HasPrefix(t, "❯") || strings.HasPrefix(t, ">")) && ruleLine.MatchString(plain[i-1]) {
			prompt = i
			break
		}
	}
	if prompt < 0 {
		return screenInfo{}
	}
	var si screenInfo
	top := prompt - 1 // the rule above it
	for i := top - 1; i >= 0 && i >= top-14; i-- {
		if spinLine.MatchString(plain[i]) {
			si.working = strings.TrimSpace(plain[i])
			for _, t := range plain[i+1 : top] {
				if todoLine.MatchString(t) {
					si.todos = append(si.todos, strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(t), "⎿ ")))
				}
			}
			break
		}
	}
	// Below the prompt's closing rule.
	below := prompt + 1
	for below < len(plain) && !ruleLine.MatchString(plain[below]) {
		below++
	}
	for _, t := range plain[min(below+1, len(plain)):] {
		if t = strings.TrimSpace(t); t != "" {
			si.status = append(si.status, t)
		}
	}
	return si
}
