package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xdeafcafe/rush/internal/community"
	"github.com/0xdeafcafe/rush/internal/host"
	"github.com/0xdeafcafe/rush/internal/jsonx"
)

func boardCLI(t *testing.T, input string, args ...string) (string, int) {
	t.Helper()
	var out, err bytes.Buffer
	code := communityCmd(args, strings.NewReader(input), &out, &err)
	return out.String() + err.String(), code
}
func TestCommunityCLIWorkflow(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	t.Setenv("RUSH_SESSION", "")
	out, code := boardCLI(t, "Question\nDetails", "ask", "Parser help", "--json")
	if code != 0 {
		t.Fatal(out)
	}
	var thread community.Thread
	if err := jsonx.Unmarshal([]byte(out), &thread); err != nil {
		t.Fatal(err)
	}
	if thread.Author.Name != "You" || thread.Messages[0].Text != "Question\nDetails" {
		t.Fatalf("human post: %+v", thread)
	}
	dir := filepath.Join(host.Root(), "verified")
	os.MkdirAll(dir, 0700)
	b, _ := jsonx.Marshal(host.Config{ID: "verified", Kind: "kimi", Name: "Investigator"})
	os.WriteFile(filepath.Join(dir, "config.json"), b, 0600)
	t.Setenv("RUSH_SESSION", "verified")
	if out, code := boardCLI(t, "Agent reply", "--json", "reply", thread.ID); code != 0 {
		t.Fatal(out)
	}
	out, code = boardCLI(t, "", "show", thread.ID, "--json")
	if code != 0 {
		t.Fatal(out)
	}
	jsonx.Unmarshal([]byte(out), &thread)
	if len(thread.Messages) != 2 || thread.Messages[1].Author.Kind != "kimi" || thread.Messages[1].Author.SessionID != "verified" {
		t.Fatalf("agent attribution: %+v", thread)
	}
	for _, verb := range []string{"resolve", "reopen"} {
		if out, code := boardCLI(t, "", verb, thread.ID); code != 0 {
			t.Fatal(out)
		}
	}
	if out, code := boardCLI(t, "", "list"); code != 0 || !strings.Contains(out, "Parser help") || !strings.Contains(out, "open") {
		t.Fatalf("list: %d %s", code, out)
	}
	t.Setenv("RUSH_SESSION", "unverifiable")
	if out, code := boardCLI(t, "must not post", "reply", thread.ID, "--json"); code == 0 || !strings.Contains(out, "cannot verify") {
		t.Fatalf("unverified author accepted: %d %s", code, out)
	}
	rows, _ := community.List()
	if len(rows[0].Messages) != 2 {
		t.Fatal("failed attribution wrote a post")
	}
	if out, code := boardCLI(t, "", "ask", "Empty"); code == 0 {
		t.Fatalf("empty post accepted: %s", out)
	}
	if out, code := boardCLI(t, "", "help"); code != 0 || !strings.Contains(out, "do not interrupt") {
		t.Fatalf("help: %s", out)
	}
}

func TestCommunityJSONListOmitsBodies(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	t.Setenv("RUSH_SESSION", "")
	q, err := community.Ask(community.Author{Name: "You"}, "Compact question", "unique full question body")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := community.Reply(q.ID, community.Author{Name: "Helper", Kind: "codex"}, "unique full reply body"); err != nil {
		t.Fatal(err)
	}
	out, code := boardCLI(t, "", "list", "--json")
	if code != 0 {
		t.Fatal(out)
	}
	if strings.Contains(out, "full question body") || strings.Contains(out, "full reply body") || strings.Contains(out, `"messages"`) {
		t.Fatalf("list includes discussion bodies: %s", out)
	}
	var summaries []communitySummary
	if err := jsonx.Unmarshal([]byte(out), &summaries); err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].ID != q.ID || summaries[0].Replies != 1 || summaries[0].Title != q.Title || summaries[0].CreatedAt.IsZero() {
		t.Fatalf("summary: %+v", summaries)
	}
	out, code = boardCLI(t, "", "show", q.ID, "--json")
	if code != 0 || !strings.Contains(out, "unique full question body") || !strings.Contains(out, "unique full reply body") {
		t.Fatalf("show lost full discussion: %d %s", code, out)
	}
}
