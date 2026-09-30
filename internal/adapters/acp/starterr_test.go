package acp

import (
	"context"
	"strings"
	"testing"
	"time"
)

// An agent that refuses the session says why in its answer: what it dumps
// on stderr (here a closing brace) isn't added to it.
func TestStartRefusedKeepsTheAgentsWords(t *testing.T) {
	script := `reply() { id=$(printf '%s' "$1" | sed 's/.*"id":\([0-9]*\).*/\1/'); printf '{"jsonrpc":"2.0","id":%s,%s}\n' "$id" "$2"; }
read -r l; reply "$l" '"result":{"protocolVersion":1}'
read -r l; echo '}' >&2; reply "$l" '"error":{"code":-32000,"message":"no key"}'
sleep 1`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Start(ctx, Options{Command: "sh", Args: []string{"-c", script}, Dir: t.TempDir()})
	if err == nil || !strings.HasSuffix(err.Error(), "no key (-32000)") {
		t.Errorf("err %v", err)
	}
}
