package hot

import (
	"os"
	"regexp"
)

var once = regexp.MustCompile(`a+`) // at package level: once

func f(pat string, names []string) string {
	re := regexp.MustCompile(`b+`) // want `regexp.MustCompile of a constant on every call`
	_ = regexp.MustCompile(pat)    // not a constant: can't be hoisted
	s := ""
	for _, n := range names {
		fh, _ := os.Open(n)
		defer fh.Close() // want `defer in a loop`
		s += n           // want `string \+= in a loop`
		func() {
			g, _ := os.Open(n)
			defer g.Close() // its own function: fine
		}()
	}
	n := 0
	for i := 0; i < 3; i++ {
		n += i // a number: fine
	}
	_, _ = re, once
	return s
}
