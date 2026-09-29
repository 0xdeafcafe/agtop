package codex

import (
	"strconv"
	"strings"

	"github.com/0xdeafcafe/rush/internal/agent/tool"
)

// parseDiff reads a unified diff's hunks as patches to path. File headers
// ("diff --git", "---", "+++", "index") are skipped; a hunk header without
// line numbers ("@@") starts a hunk at line 0.
func parseDiff(path, diff string) []tool.Patch {
	var out []tool.Patch
	var cur *tool.Patch
	remOld, remNew := 0, 0 // lines the current hunk's header says are left; -1 when it didn't say
	for _, ln := range strings.Split(strings.TrimSuffix(diff, "\n"), "\n") {
		if strings.HasPrefix(ln, "@@") {
			out = append(out, hunk(path, ln))
			cur = &out[len(out)-1]
			remOld, remNew = cur.OldLines, cur.NewLines
			continue
		}
		if cur == nil {
			continue // file headers before a hunk
		}
		if strings.HasPrefix(ln, `\`) {
			continue // \ No newline at end of file
		}
		if ln == "" {
			ln = " "
		}
		if c := ln[0]; c != ' ' && c != '-' && c != '+' {
			cur = nil
			continue
		}
		cur.Lines = append(cur.Lines, ln)
		if ln[0] != '+' && remOld > 0 {
			remOld--
		}
		if ln[0] != '-' && remNew > 0 {
			remNew--
		}
		if remOld == 0 && remNew == 0 {
			cur = nil // the hunk is whole; what follows is the next file's headers
		}
	}
	for i := range out {
		fillCounts(&out[i])
	}
	return out
}

// hunk is a patch from a header such as "@@ -12,3 +12,4 @@ func f()".
func hunk(path, header string) tool.Patch {
	p := tool.Patch{Path: path, OldLines: -1, NewLines: -1}
	for _, f := range strings.Fields(strings.TrimPrefix(header, "@@")) {
		if f == "@@" {
			break
		}
		if len(f) < 2 || (f[0] != '-' && f[0] != '+') {
			continue
		}
		start, count, hasCount := strings.Cut(f[1:], ",")
		s, _ := strconv.Atoi(start)
		n := 1
		if hasCount {
			n, _ = strconv.Atoi(count)
		}
		if f[0] == '-' {
			p.OldStart, p.OldLines = s, n
		} else {
			p.NewStart, p.NewLines = s, n
		}
	}
	return p
}

// fillCounts counts a hunk's lines when its header didn't.
func fillCounts(p *tool.Patch) {
	var old, new int
	for _, l := range p.Lines {
		if l[0] != '+' {
			old++
		}
		if l[0] != '-' {
			new++
		}
	}
	if p.OldLines < 0 {
		p.OldLines = old
	}
	if p.NewLines < 0 {
		p.NewLines = new
	}
}

// wholeFile is a file's content as one hunk: all added, or all removed.
func wholeFile(path, content string, added bool) []tool.Patch {
	if content == "" {
		return nil
	}
	sign := "-"
	if added {
		sign = "+"
	}
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	p := tool.Patch{Path: path, Lines: make([]string, len(lines))}
	for i, l := range lines {
		p.Lines[i] = sign + l
	}
	if added {
		p.NewStart, p.NewLines = 1, len(lines)
	} else {
		p.OldStart, p.OldLines = 1, len(lines)
	}
	return []tool.Patch{p}
}

// isDiff is whether s is a unified diff rather than a file's content.
func isDiff(s string) bool {
	return strings.HasPrefix(s, "@@") || strings.Contains(s, "\n@@") || strings.HasPrefix(s, "--- ") || strings.HasPrefix(s, "diff --git")
}
