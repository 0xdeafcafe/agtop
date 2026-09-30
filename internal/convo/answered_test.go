package convo

import "testing"

func TestAnsweredPairsEachQuestion(t *testing.T) {
	st := &Step{
		Tool:  "AskUserQuestion",
		Input: []byte(`{"questions":[{"question":"Which app, \"own\" or main?"},{"question":"Lint too?"}]}`),
		Output: `The user answered: "Which app, "own" or main?"="Own stack, working tree (Recommended)", ` +
			`"Lint too?"="prose now, lint after". Read the answers carefully — they may request changes.`,
	}
	qa := answered(st)
	if len(qa) != 2 || qa[0][1] != "Own stack, working tree (Recommended)" || qa[1][1] != "prose now, lint after" {
		t.Fatalf("got %q", qa)
	}
	st.Output = "something else"
	if answered(st) != nil {
		t.Fatal("an unrecognised reply should show as it came")
	}
}
