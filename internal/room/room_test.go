package room

import "testing"

func TestReady(t *testing.T) {
	for _, c := range []struct{ in, text, ready string }{
		{"I agree.\n\nREADY: yes", "I agree.", "yes"},
		{"Reliable enough. READY: no", "Reliable enough.", "no"},
		{"Done.\n**READY:** yes", "Done.", "yes"},
		{"No marker here", "No marker here", ""},
		{"READY: no at first\nthen READY: yes", "READY: no at first\nthen", "yes"},
	} {
		text, ready := Ready(c.in)
		if text != c.text || ready != c.ready {
			t.Errorf("Ready(%q) = %q, %q; want %q, %q", c.in, text, ready, c.text, c.ready)
		}
	}
}

func TestParse(t *testing.T) {
	ms := []Member{{Name: "Opus"}, {Name: "Codex"}}
	if e := Parse("@codex, why?", ms); e.To != "Codex" || e.Kind != Say {
		t.Errorf("directed: %+v", e)
	}
	if e := Parse("@nobody hi", ms); e.To != "" {
		t.Errorf("unknown name directed: %+v", e)
	}
	for in, want := range map[string]string{" /Verdict ": "verdict", "/pause": "pause", "/resume": "resume", "/stop": "stop"} {
		if e := Parse(in, ms); e.Kind != Control || e.Text != want {
			t.Errorf("control %q: %+v", in, e)
		}
	}
}

func TestName(t *testing.T) {
	// No adapters are registered here: the harness's name is the kind's.
	for _, c := range []struct{ kind, model, want string }{
		{"codex", "gpt-6-luna", "Luna"},
		{"codex", "gpt-5.5", "codex"},
		{"codex", "", "codex"},
	} {
		if got := Name(c.kind, c.model, nil); got != c.want {
			t.Errorf("Name(%q, %q) = %q, want %q", c.kind, c.model, got, c.want)
		}
	}
	if got := Name("codex", "gpt-6-luna", []string{"luna"}); got != "Luna2" {
		t.Errorf("taken name = %q, want Luna2", got)
	}
}
