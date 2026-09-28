package advisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/efficiency"
)

// The models each pass runs on, and what each may spend.
const (
	scoutModel  = "haiku"
	scoutBudget = 0.15
	scoutTime   = 5 * time.Minute

	reviewModel  = "opus"
	reviewBudget = 1.50
	reviewTime   = 10 * time.Minute
)

// Result is what one pass found and what it cost.
type Result struct {
	At       time.Time
	Findings []Finding
	Reviewed []time.Time // when each Opus review ran
	Spent    float64
	Err      error
}

// Pass runs the advisor once, as acct: Haiku proposes, then Opus checks
// the candidates worth money, new and left from earlier passes, while the
// day's reviews last. reviews is how many the cap still allows.
func Pass(ctx context.Context, acct claude.Account, in Input, reviews int) Result {
	res := Result{At: time.Now()}
	digest := Digest(in)
	dirs := []string{acct.ProjectsDir(), efficiency.Dir()}

	var scout struct {
		Findings []proposal `json:"findings"`
	}
	cost, err := ask(ctx, acct, call{
		model: scoutModel, budget: scoutBudget, timeout: scoutTime, dirs: dirs,
		system: scoutSystem, prompt: digest, schema: scoutSchema,
	}, &scout)
	res.Spent += cost
	if err != nil {
		res.Err = fmt.Errorf("haiku: %w", err)
		return res
	}

	var cands []Finding
	for _, p := range scout.Findings {
		if strings.TrimSpace(p.Title) == "" {
			continue
		}
		cands = append(cands, p.finding(Candidate, res.At))
	}
	for _, f := range in.Pending {
		if !slices.ContainsFunc(cands, func(c Finding) bool { return c.ID == f.ID }) {
			cands = append(cands, f)
		}
	}
	// The most at stake is checked first.
	slices.SortStableFunc(cands, func(a, b Finding) int {
		switch {
		case a.Weekly > b.Weekly:
			return -1
		case a.Weekly < b.Weekly:
			return 1
		}
		return 0
	})
	for i := range cands {
		c := &cands[i]
		if reviews == 0 || c.Weekly < ReviewAt {
			continue
		}
		reviews--
		res.Reviewed = append(res.Reviewed, time.Now())
		var v verdict
		cost, err := ask(ctx, acct, call{
			model: reviewModel, budget: reviewBudget, timeout: reviewTime, dirs: dirs,
			system: reviewSystem, prompt: reviewPrompt(digest, *c), schema: reviewSchema,
		}, &v)
		res.Spent += cost
		if err != nil {
			res.Err = fmt.Errorf("opus: %w", err)
			continue // it stays a candidate
		}
		if !v.Confirmed {
			c.Status, c.Note = Rejected, v.Note
			continue
		}
		// Kept under the candidate's ID, so Haiku proposing it again
		// finds it already checked.
		id := c.ID
		*c = v.proposal.finding(Confirmed, res.At)
		c.ID, c.Note = id, v.Note
	}
	res.Findings = cands
	return res
}

// proposal is a finding as a model writes it.
type proposal struct {
	Title    string   `json:"title"`
	Detail   string   `json:"detail"`
	Evidence []string `json:"evidence"`
	Weekly   float64  `json:"weeklyCost"`
	Fix      string   `json:"fix"`
	Open     string   `json:"open"`
}

func (p proposal) finding(status string, at time.Time) Finding {
	f := Finding{ID: idOf(p.Title), Title: strings.TrimSpace(p.Title), Detail: strings.TrimSpace(p.Detail),
		Evidence: p.Evidence, Weekly: max(0, p.Weekly), Open: p.Open, Status: status, At: at}
	if efficiency.Find(p.Fix) != nil {
		f.Fix = p.Fix
	}
	return f
}

type verdict struct {
	Confirmed bool   `json:"confirmed"`
	Note      string `json:"note"`
	proposal
}

// call is one `claude -p`.
type call struct {
	model   string
	budget  float64
	timeout time.Duration
	dirs    []string
	system  string
	prompt  string
	schema  string
}

// ask runs c and decodes its structured answer into out. It reports what
// the call cost even when it fails.
func ask(ctx context.Context, acct claude.Account, c call, out any) (float64, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return 0, err
	}
	args := []string{"-p",
		"--model", c.model,
		"--output-format", "json",
		"--no-session-persistence", // its own runs mustn't show up in the figures it reads
		"--strict-mcp-config", "--setting-sources", "",
		"--tools", "Read,Grep,Glob", "--allowedTools", "Read,Grep,Glob",
		"--system-prompt", c.system, "--exclude-dynamic-system-prompt-sections",
		"--max-budget-usd", fmt.Sprintf("%.2f", c.budget),
		"--json-schema", c.schema,
	}
	for _, d := range c.dirs {
		args = append(args, "--add-dir", d)
	}
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = Dir()
	cmd.Env = append(acct.Env(), "AGTOP_ADVISOR=1")
	cmd.Stdin = strings.NewReader(c.prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()

	var r struct {
		IsError    bool            `json:"is_error"`
		Subtype    string          `json:"subtype"`
		Result     string          `json:"result"`
		Cost       float64         `json:"total_cost_usd"`
		Structured json.RawMessage `json:"structured_output"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
		if runErr != nil {
			return 0, fmt.Errorf("%w: %s", runErr, firstLine(stderr.String()))
		}
		return 0, err
	}
	switch {
	case r.IsError:
		return r.Cost, fmt.Errorf("%s: %s", r.Subtype, firstLine(r.Result))
	case len(r.Structured) == 0 || string(r.Structured) == "null":
		return r.Cost, errors.New("no answer")
	}
	return r.Cost, json.Unmarshal(r.Structured, out)
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

func reviewPrompt(digest string, c Finding) string {
	b, _ := json.MarshalIndent(proposal{c.Title, c.Detail, c.Evidence, c.Weekly, c.Fix, c.Open}, "", "  ")
	return "## Candidate\n" + string(b) + "\n\n" + digest
}

const scoutSystem = `You are agtop's advisor. agtop is a terminal dashboard that runs the user's coding agents (Claude Code and others). You get a digest of the last 7 days of their agents' figures, which agtop has already worked out.

Find up to 5 specific changes to how this user works with their agents that would cut tokens and cost, or make the agents faster. Look at habits and setup: CLAUDE.md and memory, settings, savers, model and effort choice, subagent use, sessions left to grow instead of starting fresh, files read again and again, noisy shell output, and prompts that make an agent explore more than it needs.

Rules:
- Every finding rests on evidence: figures from the digest, or lines in a transcript you've read. Put transcript paths, and what you saw in them, in evidence.
- Keep reading to a minimum. Grep transcripts for patterns, and read only slices, with offset and limit. Never read a whole transcript.
- Give no generic advice. If the figures don't show a problem, return fewer findings, or none.
- Don't repeat anything under "Already said".
- title: one short line, what to change. detail: one or two sentences, why, with the figure behind it.
- weeklyCost: the dollars a week this change would plausibly save, worked out from the digest's figures. That's the share it would cut, never the whole figure it touches. Be conservative, and use 0 if you can't tell.
- fix: a saver id from the digest's list when one addresses it, otherwise "". open: the absolute path of a file the user should edit, when that's the change, otherwise "".`

const reviewSystem = `You check findings for agtop's advisor. A cheaper model read a digest of the user's coding-agent figures and proposed the candidate below. Your job is to decide whether it's true and worth the user's time.

Check its evidence against the transcripts and the digest's figures: grep and read slices, never whole transcripts. Confirm it only if the evidence holds, the change would really help this user, and the cost estimate is sound (correct it if not). Reject anything generic, already handled by a saver that's on, or unsupported.

When you confirm it, rewrite title and detail to be exact and actionable, in plain words: title is one short line, and detail is one or two sentences with the figure behind it. Keep or correct fix and open. note: one line on what you checked, or why you rejected it.`

const findingProps = `"title":{"type":"string"},"detail":{"type":"string"},"evidence":{"type":"array","items":{"type":"string"}},"weeklyCost":{"type":"number"},"fix":{"type":"string"},"open":{"type":"string"}`

var (
	scoutSchema  = `{"type":"object","properties":{"findings":{"type":"array","maxItems":5,"items":{"type":"object","properties":{` + findingProps + `},"required":["title","detail","evidence","weeklyCost","fix","open"]}}},"required":["findings"]}`
	reviewSchema = `{"type":"object","properties":{"confirmed":{"type":"boolean"},"note":{"type":"string"},` + findingProps + `},"required":["confirmed","note","title","detail","evidence","weeklyCost","fix","open"]}`
)
