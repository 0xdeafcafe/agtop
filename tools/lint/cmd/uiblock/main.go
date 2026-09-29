// Command uiblock lists everything agtop's UI goroutine can reach that
// waits: a system call (the disk, a process, the network) or a sleep. It
// builds the whole program's call graph (VTA, so calls through interfaces
// and func values count) and walks it from ui.New and the Model's Init,
// Update and View. What runs in a goroutine, or in a tea.Cmd handed back
// to bubbletea, isn't called there, so it isn't walked.
//
//	cd tools/lint && go run ./cmd/uiblock ../..
//
// A function marked //uiblock:nowait in its doc, saying why, is taken
// at its word: the walk stops there.
//
// Each line is an agtop function that calls out to something that waits,
// with the shortest path from the UI to it. It exits 1 when there's any.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"os"
	"sort"
	"strings"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/cha"
	"golang.org/x/tools/go/callgraph/vta"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

const mod = "github.com/0xdeafcafe/agtop"

// harmless are system calls that answer from the process itself.
var harmless = map[string]bool{
	"Getenv": true, "Setenv": true, "Unsetenv": true, "Environ": true, "Clearenv": true, "Getpid": true, "Getppid": true,
	"Getuid": true, "Geteuid": true, "Getgid": true, "Getegid": true, "Getpagesize": true, "Exit": true, "runtimeSetenv": true,
	"runtimeUnsetenv": true, "Kill": true, "Getwd": true, // the kernel's word, not the disk's
}

func waits(f *ssa.Function) bool {
	if f.Pkg == nil {
		return false
	}
	p, name := f.Pkg.Pkg.Path(), f.Name()
	switch p {
	case "syscall", "internal/syscall/unix", "golang.org/x/sys/unix":
		return !harmless[name] && f.Signature.Recv() == nil
	case "time":
		return name == "Sleep"
	}
	return false
}

func main() {
	verbose := flag.Bool("v", false, "print every path, not one per function")
	flag.Parse()
	dir := "."
	if flag.NArg() > 0 {
		dir = flag.Arg(0)
	}
	cfg := &packages.Config{Mode: packages.LoadAllSyntax, Dir: dir}
	pkgs, err := packages.Load(cfg, "./cmd/agtop")
	if err != nil || packages.PrintErrors(pkgs) > 0 {
		fmt.Fprintln(os.Stderr, "uiblock: loading:", err)
		os.Exit(2)
	}
	prog, _ := ssautil.AllPackages(pkgs, ssa.InstantiateGenerics)
	prog.Build()
	fns := ssautil.AllFunctions(prog)
	cg := vta.CallGraph(fns, cha.CallGraph(prog))
	cg.DeleteSyntheticNodes()

	var roots []*callgraph.Node
	for f, n := range cg.Nodes {
		if f == nil || f.Pkg == nil || f.Pkg.Pkg.Path() != mod+"/internal/ui" {
			continue
		}
		if f.Name() == "New" && f.Signature.Recv() == nil {
			roots = append(roots, n)
		}
		if recv := f.Signature.Recv(); recv != nil && strings.HasSuffix(recv.Type().String(), "ui.Model") {
			switch f.Name() {
			case "Init", "Update", "View":
				roots = append(roots, n)
			}
		}
	}
	// A function whose doc says //uiblock:nowait (and why) never waits in
	// the view, however it could elsewhere: a save handed to a writer, a
	// look answered from what was found. The walk stops there.
	nowait := map[*ssa.Function]bool{}
	for f := range cg.Nodes {
		if f == nil {
			continue
		}
		if d, ok := f.Syntax().(*ast.FuncDecl); ok && d.Doc != nil {
			for _, c := range d.Doc.List { // Text() drops directives
				if strings.HasPrefix(c.Text, "//uiblock:nowait") {
					nowait[f] = true
				}
			}
		}
	}
	// Everything that can end up waiting, walking back from what waits.
	blocks := map[*callgraph.Node]bool{}
	var back []*callgraph.Node
	for _, n := range cg.Nodes {
		if n.Func != nil && waits(n.Func) {
			blocks[n] = true
			back = append(back, n)
		}
	}
	for len(back) > 0 {
		n := back[len(back)-1]
		back = back[:len(back)-1]
		for _, e := range n.In {
			if _, isGo := e.Site.(*ssa.Go); isGo || blocks[e.Caller] || pure(e) || nowait[e.Caller.Func] {
				continue
			}
			blocks[e.Caller] = true
			back = append(back, e.Caller)
		}
	}
	// Forward from the UI, breadth first, through the ui package only:
	// each call out of it to something that can wait is one to move.
	ui := func(n *callgraph.Node) bool {
		return n.Func.Pkg != nil && n.Func.Pkg.Pkg.Path() == mod+"/internal/ui"
	}
	from := map[*callgraph.Node]*callgraph.Edge{}
	seen := map[*callgraph.Node]bool{}
	queue := append([]*callgraph.Node(nil), roots...)
	for _, r := range roots {
		seen[r] = true
	}
	type hit struct {
		at   *callgraph.Node
		call *callgraph.Edge
	}
	var hits []hit
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, e := range n.Out {
			if _, isGo := e.Site.(*ssa.Go); isGo || !blocks[e.Callee] || pure(e) {
				continue
			}
			if r := e.Callee.Func.Signature.Recv(); r != nil && (e.Callee.Func.Name() == "Error" || e.Callee.Func.Name() == "String") {
				continue // an error or a value put in words: see pure
			}
			if !ui(e.Callee) {
				hits = append(hits, hit{n, e})
				continue
			}
			if !seen[e.Callee] {
				seen[e.Callee] = true
				from[e.Callee] = e
				queue = append(queue, e.Callee)
			}
		}
	}
	lines := map[string]bool{}
	var out []string
	for _, h := range hits {
		pos := prog.Fset.Position(h.call.Site.Pos())
		l := fmt.Sprintf("%s:%d %s → %s", strings.TrimPrefix(pos.Filename, dirAbs(dir)), pos.Line, short(h.at.Func), short(h.call.Callee.Func))
		if *verbose {
			var path []string
			for n := h.at; n != nil; {
				path = append(path, short(n.Func))
				e := from[n]
				if e == nil {
					break
				}
				n = e.Caller
			}
			for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
				path[i], path[j] = path[j], path[i]
			}
			l += "\n    " + strings.Join(path, " → ")
		}
		if !lines[l] {
			lines[l] = true
			out = append(out, l)
		}
	}
	sort.Strings(out)
	for _, l := range out {
		fmt.Println(l)
	}
	if len(out) > 0 {
		fmt.Fprintf(os.Stderr, "uiblock: %d calls on the UI goroutine can wait\n", len(out))
		os.Exit(1)
	}
}

// pureStd are standard packages that only work on what they're handed:
// they wait only when handed something that does (a file as an io.Reader,
// an error whose Error reads one), and the call graph can't tell which,
// so it would link every fmt.Sprintf to every Error method there is. What
// really waits opens the file or runs the command first, and that's found.
var pureStd = []string{"fmt", "errors", "strconv", "strings", "bytes", "unicode", "sort", "slices", "maps", "math",
	"time", "internal/", "reflect", "regexp", "encoding/", "io", "bufio", "text/", "html", "sync", "context", "log/slog",
	"unique", "iter", "hash", "crypto/", "container/", "cmp", "path", "vendor/", "mime", "net/url"}

func pure(e *callgraph.Edge) bool {
	// A call through a func value reaches, as VTA sees it, every func of
	// its type that flows anywhere near: kept to the caller's own package,
	// where the UI's closures (a picker's act, a sheet's apply) are made.
	if c := e.Site.Common(); !c.IsInvoke() && c.StaticCallee() == nil && e.Caller.Func.Pkg != nil && e.Callee.Func.Pkg != nil &&
		e.Caller.Func.Pkg != e.Callee.Func.Pkg && e.Callee.Func.Parent() != nil {
		return true
	}
	if c := e.Site.Common(); c.IsInvoke() {
		switch c.Method.Name() {
		case "Error", "String", "GoString", "Format", "Unwrap", "Is", "As":
			return true
		}
	}
	f := e.Caller.Func
	if f.Pkg == nil {
		return false
	}
	p := f.Pkg.Pkg.Path()
	if strings.Contains(p, ".") && !strings.HasPrefix(p, "vendor/") {
		return false // not the standard library
	}
	if p == "time" && f.Name() == "Sleep" {
		return false
	}
	for _, q := range pureStd {
		if p == q || strings.HasPrefix(p, q+"/") || strings.HasSuffix(q, "/") && strings.HasPrefix(p, q) {
			return true
		}
	}
	return false
}

func short(f *ssa.Function) string {
	return strings.ReplaceAll(f.String(), mod+"/internal/", "")
}

func dirAbs(d string) string {
	wd, _ := os.Getwd()
	if !strings.HasPrefix(d, "/") {
		d = wd + "/" + d
	}
	parts := []string{}
	for _, p := range strings.Split(d, "/") {
		switch p {
		case "", ".":
		case "..":
			parts = parts[:len(parts)-1]
		default:
			parts = append(parts, p)
		}
	}
	return "/" + strings.Join(parts, "/") + "/"
}
