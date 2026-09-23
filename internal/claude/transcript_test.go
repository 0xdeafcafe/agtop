package claude

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

const block1 = `{"type":"assistant","timestamp":"2026-09-23T10:00:00Z","message":{"id":"m1","model":"claude-opus-5-5","content":[{"type":"thinking"}],"usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":1000,"cache_creation_input_tokens":200,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":200}}}}`
const block2 = `{"type":"assistant","timestamp":"2026-09-23T10:00:01Z","message":{"id":"m1","model":"claude-opus-5-5","content":[{"type":"text","text":"see https://github.com/a/b/pull/42"}],"usage":{"input_tokens":10,"output_tokens":50,"cache_read_input_tokens":1000,"cache_creation_input_tokens":200,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":200}}}}`
const user = `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"x"}]}}`
const next = `{"type":"assistant","timestamp":"2026-09-23T10:01:00Z","message":{"id":"m2","model":"claude-opus-5-5","content":[{"type":"text","text":"done"}],"usage":{"input_tokens":1,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`

func TestScanCountsEachMessageOnceAndResumes(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(p, []byte(block1+"\n"+block2+"\n"+user+"\n"+next[:40]), 0o600); err != nil {
		t.Fatal(err)
	}
	var tot Totals
	if _, err := Scan(p, &tot, nil); err != nil {
		t.Fatal(err)
	}
	c, u := tot.Spend()
	// m1 counted once with its final usage: 10 in, 50 out, 1000 read, 200 1h write.
	want := (10*4 + 50*20 + 1000*0.20 + 200*4*2) / 1e6
	if math.Abs(c-want) > 1e-12 || u.Output != 50 {
		t.Fatalf("cost %v want %v, usage %+v", c, want, u)
	}
	if len(tot.PRs) != 1 || tot.PRs[0] != "https://github.com/a/b/pull/42" {
		t.Fatalf("prs %v", tot.PRs)
	}
	// The half-written line is not consumed until it completes.
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(next[40:] + "\n")
	f.Close()
	if _, err := Scan(p, &tot, nil); err != nil {
		t.Fatal(err)
	}
	c2, u2 := tot.Spend()
	if u2.Output != 51 || c2 <= c {
		t.Fatalf("after resume: %v %+v", c2, u2)
	}
	if d := tot.DaySpend(Day(tot.Last)); math.Abs(d-c2) > 1e-12 {
		t.Fatalf("day spend %v want %v", d, c2)
	}
}

func TestPricing(t *testing.T) {
	if p, _ := PriceFor("claude-opus-5-5"); p.Input != 4 {
		t.Fatalf("opus 5.5 %+v", p)
	}
	if p, _ := PriceFor("claude-opus-5"); p.Input != 5 {
		t.Fatalf("opus 5 %+v", p)
	}
	if p, _ := PriceFor("claude-haiku-4-5-20251001"); p.Input != 1 {
		t.Fatalf("haiku %+v", p)
	}
	if _, ok := PriceFor("gpt-5"); ok {
		t.Fatal("unknown model priced")
	}
	u := TokenUsage{CacheRead: 1e6}
	if c := Cost("claude-sonnet-5", u, false); math.Abs(c-0.2) > 1e-12 {
		t.Fatalf("default cache read is 0.1x input: %v", c)
	}
	if c := Cost("claude-opus-5-5", TokenUsage{Output: 1e6}, true); c != 40 {
		t.Fatalf("fast mode doubles: %v", c)
	}
}

func TestProjectSlug(t *testing.T) {
	if s := ProjectSlug("/Users/lw/Source/github.com/langwatch/langwatch"); s != "-Users-lw-Source-github-com-langwatch-langwatch" {
		t.Fatal(s)
	}
}
