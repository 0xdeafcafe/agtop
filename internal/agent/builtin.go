package agent

import "path/filepath"

// Builtin is an adapter whose sessions the core still runs and reads
// itself, as it did before agtop ran other agents: the host drives them
// through headless and sends clients their own stream-json lines, and
// fleet and convo find and read their transcripts directly. Only Claude
// Code's is, until the rest of docs/multi-agent.md's refactor moves all
// that behind its adapter; then this goes.
type Builtin interface {
	Builtin()
}

// BuiltinKind is the kind of the built-in agent; empty when none is
// registered.
func BuiltinKind() Kind {
	for _, a := range All() {
		if _, ok := a.(Builtin); ok {
			return a.Kind()
		}
	}
	return ""
}

// LegacyKind is the agent an empty kind meant: agtop ran only Claude Code
// before it ran others, and wrote no kind for it.
const LegacyKind Kind = "claude" // migration: what older state meant by no kind

// Migrated is a kind read from state, a host's info or config an older
// agtop wrote, as it's kept now: an empty one is LegacyKind. Call it only
// where such a kind is read in; nothing past there sees an empty kind.
func Migrated(kind string) Kind {
	if kind == "" {
		return LegacyKind // migration: older agtops wrote no kind for Claude Code
	}
	return Kind(kind)
}

// IsBuiltin is whether agent k is the built-in one. Empty is, whether or
// not it's registered.
func IsBuiltin(k Kind) bool {
	if k == "" {
		return true
	}
	a, ok := Get(k)
	if !ok {
		return false
	}
	_, ok = a.(Builtin)
	return ok
}

// ProgramOf is what agent k's program is called; empty when it has none,
// isn't registered, or rides another's.
func ProgramOf(k Kind) string {
	a, ok := Get(k)
	if !ok {
		return ""
	}
	if _, ok := a.(Rider); ok {
		return ""
	}
	p, ok := a.(Programmer)
	if !ok {
		return ""
	}
	name, _ := p.Program()
	return name
}

// IsProgram is whether a process called comm (a name, or a path) is one
// of the registered agents' programs.
func IsProgram(comm string) bool {
	if comm = filepath.Base(comm); comm == "" || comm == "." || comm == "/" {
		return false
	}
	for _, a := range All() {
		if ProgramOf(a.Kind()) == comm {
			return true
		}
	}
	return false
}
