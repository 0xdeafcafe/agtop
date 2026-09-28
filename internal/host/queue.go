package host

import (
	"fmt"
	"strings"
)

// JoinQueue makes queued messages one message that still reads as several:
// each is numbered under its own heading, so the agent doesn't take them
// for one run-on thought. A lone message goes as it is.
func JoinQueue(items []string) string {
	if len(items) == 1 {
		return items[0]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d messages, queued while you worked. Each is its own message; take them in order.", len(items))
	for i, it := range items {
		fmt.Fprintf(&b, "\n\n[Message %d of %d]\n%s", i+1, len(items), it)
	}
	return b.String()
}
