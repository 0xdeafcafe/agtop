package secrets

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// match is one regex match a rule accepted, with byte offsets in the text
// scanned.
type match struct {
	start, end int // the regex match
	groups     []string
	locs       []int
	// before is how much of the credential sits in front of the match (a
	// URL's scheme).
	before int
}

func (r *rule) hasLookarounds() bool { return r.tokenBounded || r.schemeBefore }

// find returns every match of r in text that passes its boundary checks and
// its accept test.
func (r *rule) find(text string) []match {
	if r.precondition != nil && !r.precondition(text) {
		return nil
	}
	var out []match
	keep := func(loc []int) {
		groups := make([]string, len(loc)/2)
		for i := range groups {
			if loc[2*i] >= 0 {
				groups[i] = text[loc[2*i]:loc[2*i+1]]
			}
		}
		if r.accept != nil && !r.accept(groups) {
			return
		}
		m := match{start: loc[0], end: loc[1], groups: groups, locs: loc}
		if r.schemeBefore {
			m.before = schemeLength(text, loc[0])
		}
		out = append(out, m)
	}
	if !r.hasLookarounds() {
		// These rules may use \b and ^, so they search the whole text.
		for _, loc := range r.re.FindAllStringSubmatchIndex(text, -1) {
			if r.notFollowedBy != "" && loc[1] < len(text) && strings.IndexByte(r.notFollowedBy, text[loc[1]]) >= 0 {
				continue
			}
			keep(loc)
		}
		return out
	}
	// A match that fails a boundary check is no match at that position, so
	// the search goes on from the next character, as a lookaround would.
	pos := 0
	for pos <= len(text) {
		loc := r.re.FindStringSubmatchIndex(text[pos:])
		if loc == nil {
			break
		}
		for i := range loc {
			if loc[i] >= 0 {
				loc[i] += pos
			}
		}
		start, end := loc[0], loc[1]
		if !r.boundsOK(text, start, end) {
			_, size := utf8.DecodeRuneInString(text[start:])
			pos = start + max(size, 1)
			continue
		}
		keep(loc)
		if end == start {
			end++
		}
		pos = end
	}
	return out
}

func (r *rule) boundsOK(text string, start, end int) bool {
	if r.tokenBounded {
		if start > 0 && isTokenByte(text[start-1]) || end < len(text) && isTokenByte(text[end]) {
			return false
		}
	}
	if r.schemeBefore && schemeLength(text, start) == 0 {
		return false
	}
	return true
}

func isSchemeByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '+' || c == '.' || c == '-'
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// schemeLength is the length of the URL scheme ending at at: a letter then
// up to 30 of [a-z0-9+.-], the longest such run. 0 when there is none.
func schemeLength(text string, at int) int {
	best := 0
	for j := 1; j <= 31 && at-j >= 0; j++ {
		c := text[at-j]
		if !isSchemeByte(c) {
			break
		}
		if isLetter(c) {
			best = j
		}
	}
	return best
}

// secretSpan is where the secret itself sits in m: its own group for the
// rules that keep context, else the match up to its first quote.
func (r *rule) secretSpan(m match) (int, int) {
	if r.secretGroup > 0 {
		return m.locs[2*r.secretGroup], m.locs[2*r.secretGroup+1]
	}
	return m.start, m.start + keptLengthAtBoundary(m.groups[0])
}

// keptLengthAtBoundary is the length of a match up to its first quote or
// backtick: a secret never holds one, so a greedy pattern stops there.
func keptLengthAtBoundary(s string) int {
	if i := strings.IndexAny(s, "\"'`"); i >= 0 {
		return i
	}
	return len(s)
}

// Options narrow a scan.
type Options struct {
	// CustomPatterns run after the built-in rules; see CompilePatterns.
	CustomPatterns []*Pattern
	// SkipRuleIDs leaves built-in rules out; custom patterns always run.
	SkipRuleIDs []string
}

// Redact replaces every secret in text with Marker and says how many it
// replaced. Context the rules keep (a URL's user and host, a Bearer prefix)
// stays.
func Redact(text string, o Options) (string, int) {
	if text == "" {
		return text, 0
	}
	if len(text) > maxScanLength {
		var b strings.Builder
		total := 0
		for _, s := range sliceForScan(text) {
			t, n := redactSlice(s, o)
			b.WriteString(t)
			total += n
		}
		return b.String(), total
	}
	return redactSlice(text, o)
}

func redactSlice(text string, o Options) (string, int) {
	count := 0
	for i := range valueRules {
		r := &valueRules[i]
		if slices.Contains(o.SkipRuleIDs, r.id) {
			continue
		}
		ms := r.find(text)
		if len(ms) == 0 {
			continue
		}
		var b strings.Builder
		last := 0
		for _, m := range ms {
			var from, to int
			if r.render {
				from, to = r.secretSpan(m)
			} else {
				kept := keptLengthAtBoundary(m.groups[0])
				if kept == 0 {
					continue
				}
				from, to = m.start, m.start+kept
			}
			b.WriteString(text[last:from])
			b.WriteString(Marker)
			last = to
			count++
		}
		b.WriteString(text[last:])
		text = b.String()
	}
	for _, p := range o.CustomPatterns {
		var b strings.Builder
		last := 0
		for _, sp := range p.spans(text) {
			b.WriteString(text[last:sp[0]])
			b.WriteString(Marker)
			last = sp[1]
			count++
		}
		b.WriteString(text[last:])
		text = b.String()
	}
	return text, count
}

// Match is one secret Detect found.
type Match struct {
	// RuleID is the built-in rule, or custom_pattern.
	RuleID      string
	Description string
	// Start and End span the whole credential in the text, in bytes.
	Start, End int
	// SecretStart and SecretEnd span the secret itself: the password in a
	// connection URL, the value after a credential's name.
	SecretStart, SecretEnd int
}

// Detect reports the secrets in text without changing it. Layers overlap by
// design, so one credential is reported once, under the most specific rule.
// Text past the scan budget reports nothing; Find slices it instead.
func Detect(text string, o Options) []Match {
	if text == "" || len(text) > maxScanLength {
		return nil
	}
	return detectSlice(text, o)
}

func detectSlice(text string, o Options) []Match {
	var all []Match
	for i := range valueRules {
		r := &valueRules[i]
		if slices.Contains(o.SkipRuleIDs, r.id) {
			continue
		}
		for _, m := range r.find(text) {
			length := m.end - m.start
			if !r.render {
				length = keptLengthAtBoundary(m.groups[0])
			}
			if length == 0 {
				continue
			}
			ss, se := r.secretSpan(m)
			all = append(all, Match{
				RuleID: r.id, Description: r.description,
				Start: m.start - m.before, End: m.start + length,
				SecretStart: ss, SecretEnd: se,
			})
		}
	}
	for _, p := range o.CustomPatterns {
		for _, sp := range p.spans(text) {
			all = append(all, Match{RuleID: "custom_pattern", Description: "Custom secret pattern",
				Start: sp[0], End: sp[1], SecretStart: sp[0], SecretEnd: sp[1]})
		}
	}
	var kept []Match
	for _, m := range all {
		if !slices.ContainsFunc(kept, func(o Match) bool { return m.Start < o.End && o.Start < m.End }) {
			kept = append(kept, m)
		}
	}
	slices.SortStableFunc(kept, func(a, b Match) int { return a.Start - b.Start })
	return kept
}

const (
	safeCutLookahead = 4096
	pemBegin         = "-----BEGIN"
	pemEnd           = "-----END"
)

// sliceForScan cuts oversized text on whitespace, so no cut splits a
// credential; a PEM block is kept whole.
func sliceForScan(text string) []string {
	if len(text) <= maxScanLength {
		return []string{text}
	}
	var out []string
	for start := 0; start < len(text); {
		end := sliceEndAfter(text, start)
		out = append(out, text[start:end])
		start = end
	}
	return out
}

func sliceEndAfter(text string, start int) int {
	target := min(start+maxScanLength, len(text))
	if target >= len(text) {
		return len(text)
	}
	var end int
	if last := strings.LastIndexFunc(text[start:target], unicode.IsSpace); last > 0 {
		end = start + last
	} else {
		// One unbroken run: look ahead for whitespace, but no further than
		// any credential is long.
		look := text[target:min(target+safeCutLookahead, len(text))]
		if next := strings.IndexFunc(look, unicode.IsSpace); next >= 0 {
			end = target + next
		} else {
			end = min(target+safeCutLookahead, len(text))
		}
	}
	begin := strings.LastIndex(text[:min(end+len(pemBegin), len(text))], pemBegin)
	if begin >= start {
		if close := strings.Index(text[begin:], pemEnd); close >= 0 && begin+close >= end {
			end = begin + close + len(pemEnd)
		}
	}
	return end
}

// Pattern is a caller's own secret pattern, compiled by CompilePatterns.
type Pattern struct {
	re *regexp.Regexp
	// guarded patterns may not start inside a word: the character before a
	// match must not be [A-Za-z0-9_].
	guarded bool
}

// selfAnchored patterns say where they may start, so they get no guard.
var selfAnchored = regexp.MustCompile(`^(?:\^|\\b|\\B|\(\?<[=!])`)

// compilePattern compiles one caller pattern, case-insensitive. One that
// does not say where it may start gets a word-start guard, so `sk-.*` does
// not fire inside `task-notification`.
func compilePattern(pattern string) (*Pattern, error) {
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return nil, err
	}
	return &Pattern{re: re, guarded: !selfAnchored.MatchString(pattern)}, nil
}

// CompilePatterns compiles caller patterns, dropping any that do not compile.
func CompilePatterns(patterns []string) []*Pattern {
	var out []*Pattern
	for _, p := range patterns {
		if c, err := compilePattern(p); err == nil {
			out = append(out, c)
		}
	}
	return out
}

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

// customBoundary ends a custom match: a credential never holds whitespace,
// a quote or an angle bracket, so a trailing .* stops there.
const customBoundary = " \t\n\r\f\v\"'`<>"

// locs are the raw [start, end) matches of the pattern in text.
func (p *Pattern) locs(text string) [][2]int {
	var out [][2]int
	if !p.guarded {
		for _, loc := range p.re.FindAllStringIndex(text, -1) {
			out = append(out, [2]int{loc[0], loc[1]})
		}
		return out
	}
	for pos := 0; pos <= len(text); {
		loc := p.re.FindStringIndex(text[pos:])
		if loc == nil {
			break
		}
		start, end := loc[0]+pos, loc[1]+pos
		if start > 0 && isWordByte(text[start-1]) {
			_, size := utf8.DecodeRuneInString(text[start:])
			pos = start + max(size, 1)
			continue
		}
		out = append(out, [2]int{start, end})
		if end == start {
			end++
		}
		pos = end
	}
	return out
}

// spans are the matches clamped at their first customBoundary character,
// dropping the ones left empty.
func (p *Pattern) spans(text string) [][2]int {
	var out [][2]int
	for _, l := range p.locs(text) {
		kept := l[1] - l[0]
		if i := strings.IndexAny(text[l[0]:l[1]], customBoundary); i >= 0 {
			kept = i
		}
		if kept > 0 {
			out = append(out, [2]int{l[0], l[0] + kept})
		}
	}
	return out
}

// Text a pattern must never match: prose, a transcript tag, a path, a
// timestamp, a model name, and the identifiers a transcript is made of.
var ordinaryTextProbes = []string{
	"the user asked the agent to summarise the meeting notes",
	"<task-notification>",
	"modules/trace/process/src/services/trace-legacy-read.service.ts",
	"2026-08-10T14:32:11.482Z",
	"claude-opus-5",
	"fix in commit 51d07b547d0a8f3e2c1b9d4a6e7f8091a2b3c4d5",
	"id 550e8400-e29b-41d4-a716-446655440000 done",
	"traceparent: 00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
}

// OverBroadProbe returns the first ordinary text a custom pattern would
// match, or "" when it only matches credential-shaped strings. A blank or
// uncompilable pattern returns "".
func OverBroadProbe(pattern string) string {
	if strings.TrimSpace(pattern) == "" {
		return ""
	}
	p, err := compilePattern(pattern)
	if err != nil {
		return ""
	}
	for _, probe := range ordinaryTextProbes {
		if len(p.locs(probe)) > 0 {
			return probe
		}
	}
	return ""
}

var sensitiveKey = regexp.MustCompile(`(?i)(?:^|[._-])(?:password|passwd|pwd|secret|api[_-]?key|apikey|access[_-]?token|auth[_-]?token|authorization|auth|bearer|credentials?|private[_-]?key|client[_-]?secret|db[_-]?password|connection[_-]?string|session[_-]?token|refresh[_-]?token|set[_-]?cookie|cookie|x-api-key)(?:$|[._-])`)

// Nouns that name a credential whatever sits beside them; key and token
// need a qualifier.
var credentialNouns = setOf("password", "passwd", "pwd", "secret", "authorization", "auth", "bearer",
	"credential", "credentials", "cookie")

var (
	camelLower = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	camelUpper = regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)
	nonAlnum   = regexp.MustCompile(`[^A-Za-z0-9]+`)
)

// attributeWords splits a name on separators and camelCase boundaries.
func attributeWords(name string) []string {
	name = camelLower.ReplaceAllString(name, "$1 $2")
	name = camelUpper.ReplaceAllString(name, "$1 $2")
	var out []string
	for _, w := range nonAlnum.Split(name, -1) {
		if w != "" {
			out = append(out, strings.ToLower(w))
		}
	}
	return out
}

// IsSensitiveAttributeKey says whether an attribute or field name says it
// holds a credential.
func IsSensitiveAttributeKey(key string) bool {
	if sensitiveKey.MatchString(key) {
		return true
	}
	words := attributeWords(key)
	for i, w := range words {
		if credentialNouns[w] {
			return true
		}
		if (w == "key" || w == "token") && i > 0 && credentialQualifierSet[words[i-1]] {
			return true
		}
	}
	return false
}
