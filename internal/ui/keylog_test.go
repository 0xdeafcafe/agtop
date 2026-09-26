package ui

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// With the key log on, each enter is logged with the state before and
// after it, and a typed character is logged only as "·".
func TestKeylogRecordsEnter(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGTOP_HOME", home)
	t.Setenv("AGTOP_KEYLOG", "1")
	keylog.once, keylog.f = sync.Once{}, nil
	t.Cleanup(func() {
		FlushDrafts()
		if keylog.f != nil {
			_ = keylog.f.Close()
		}
		keylog.once, keylog.f = sync.Once{}, nil
	})
	writeSession(t, "aaaa1111", "the solo session")
	m := NewSolo(state.Load(), "test", "aaaa1111")
	m.Frame(160, 45)
	m.host = &hostConn{key: m.soloKey, sess: convo.New(), open: map[string]bool{}}
	m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	b, err := os.ReadFile(filepath.Join(home, "keylog.log"))
	if err != nil {
		t.Fatal(err)
	}
	log := string(b)
	for _, want := range []string{"key ·", "key enter(code=13", "before ", "box=1", "after  ", "box=0"} {
		if !strings.Contains(log, want) {
			t.Fatalf("log misses %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, "key x") {
		t.Fatalf("typed text in the log:\n%s", log)
	}
}
