package host

import (
	"github.com/0xdeafcafe/rush/internal/agent/event"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExchangeJournalCompactsAndToleratesTruncatedTail(t *testing.T) {
	setup(t)
	id := "journal"
	if err := os.MkdirAll(dir(id), 0700); err != nil {
		t.Fatal(err)
	}
	text := strings.Repeat("x", 1<<20)
	for i := 0; i < 12; i++ {
		e := event.Exchange{ID: string(rune('a' + i)), Text: text, At: time.Now(), Direction: "sent"}
		if err := appendExchange(id, e); err != nil {
			t.Fatal(err)
		}
	}
	p := filepath.Join(dir(id), "exchanges.jsonl")
	st, _ := os.Stat(p)
	if st.Size() > exchangeJournalBytes {
		t.Fatalf("unbounded journal: %d", st.Size())
	}
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString(`{"ID":`)
	f.Close()
	got := ReadExchanges(id)
	if len(got) == 0 || got[len(got)-1].ID != "l" || got[len(got)-1].Text != text || !got[0].Archived {
		t.Fatal("lost complete records after compaction/truncated write")
	}
}
