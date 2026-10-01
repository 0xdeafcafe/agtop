package convo

import (
	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/host"
	"os"
	"strings"
	"testing"
)

func TestUserMessageAcknowledgement(t *testing.T) {
	s := New()
	ack := func(id, text string) {
		s.Apply(event.Message{Role: "user", ID: id, Parts: []event.Part{{Kind: event.Text, Text: text}}}, at(2))
	}
	s.Apply(host.Sent{Text: "first"}, at(0))
	ack("one", "first")
	ack("one", "first")
	if len(s.Turns) != 1 || len(s.Turns[0].Items) != 0 {
		t.Fatalf("ack duplicated initial prompt: %+v", s.UserMessages())
	}
	s.Apply(host.Sent{Text: "also this"}, at(3))
	ack("two", "also this")
	s.Apply(host.Sent{Text: "also this"}, at(4))
	ack("three", "also this")
	msgs := s.UserMessages()
	if len(msgs) != 3 || msgs[1].Text != "also this" || msgs[2].Ref == msgs[1].Ref {
		t.Fatalf("real repeated steering lost: %+v", msgs)
	}
	s.Apply(event.TurnEnd{Reason: "done"}, at(5))
	s.Apply(host.Sent{Text: "first"}, at(6))
	ack("four", "first")
	if len(s.Turns) != 2 || len(s.UserMessages()) != 4 {
		t.Fatal("same words in a new turn must survive")
	}
}
func TestUserMessageImages(t *testing.T) {
	s := New()
	pic := &event.ImageData{MediaType: "image/png", Data: []byte("image bytes")}
	s.Apply(host.Sent{Text: "look", Images: []string{"/tmp/original.png"}}, at(0))
	s.Apply(event.Message{Role: "user", ID: "image", Parts: []event.Part{{Kind: event.Text, Text: "look"}, {Kind: event.Image, Image: pic}}}, at(1))
	msgs := s.UserMessages()
	if len(msgs) != 1 || len(msgs[0].Pictures) != 1 || msgs[0].Pictures[0] != pic || msgs[0].Images[0] != "/tmp/original.png" {
		t.Fatalf("attachment/ack lost: %+v", msgs)
	}
	s.Apply(event.TurnEnd{Reason: "done"}, at(2))
	s.Apply(event.Message{Role: "user", Parts: []event.Part{{Kind: event.Image, Image: pic}}}, at(3))
	if len(s.UserMessages()) != 2 {
		t.Fatal("image-only message lost")
	}
}
func TestNativeUserPicture(t *testing.T) {
	path := t.TempDir() + "/session.jsonl"
	if err := os.WriteFile(path, []byte(`{"type":"user","isSidechain":true,"timestamp":"2026-09-23T20:00:00Z","message":{"role":"user","content":[{"type":"text","text":"look"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aW1hZ2U="}}]}}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	tail := SubagentTail(path)
	if _, err := tail.Read(); err != nil {
		t.Fatal(err)
	}
	msgs := tail.Sess.UserMessages()
	if len(msgs) != 1 || len(msgs[0].Pictures) != 1 || string(msgs[0].Pictures[0].Data) != "image" {
		t.Fatalf("native attachment unavailable: %+v", msgs)
	}
}
func TestMessagePastesAndLinks(t *testing.T) {
	for _, text := range []string{"<pasted_content>\nfirst\nlast\n</pasted_content>", "<pasted_content id=\"x\">\nfirst\nlast\n</pasted_content id=\"x\">"} {
		if got := ExpandedMessage(text); !strings.Contains(got, "first\nlast") || strings.Contains(got, "pasted_content") {
			t.Fatalf("paste not expanded: %q", got)
		}
		linked := messageLinks(FoldPastes(text), "t1")
		if !strings.Contains(linked, "rush:message/t1") {
			t.Fatalf("paste not clickable: %q", linked)
		}
	}
}

func TestRenderedMessageAttachmentLinks(t *testing.T) {
	s := New()
	long := strings.Repeat("pasted line\n", 20)
	s.Apply(host.Sent{Text: "look [Image #1]\n<pasted_content>\n" + long + "</pasted_content>", Images: []string{"/tmp/img.png"}}, at(0))
	s.Apply(host.Sent{Text: "steering", Images: []string{"/tmp/next.png"}}, at(1))
	var all strings.Builder
	for _, line := range s.Render(Options{Width: 100, Now: at(2)}) {
		all.WriteString(line.Text)
	}
	for _, want := range []string{"rush:message/t1/image/1", "rush:message/t1\x1b", "rush:message/t1:u:0/image/1"} {
		if !strings.Contains(all.String(), want) {
			t.Errorf("missing %q in rendered attachment links", want)
		}
	}
}
