package convo

import "testing"

func TestPasteChip(t *testing.T) {
	for _, c := range []struct{ text, want string }{
		{"line1\nline2\nline3\nline4", "[pasted text #1 · 4 lines: line1 … line4]"},
		{"   \n\tfunc main() {\n\t\treturn [x]\n\t}\n", "[pasted text #1 · 4 lines: func … turn x }]"},
		{"package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(1) }\n", "[pasted text #1 · 5 lines: package … tln(1) }]"},
		{"╭─ rush ──╮\n│ ✓ ⌕ loaded SendMessage from the fleet, and a good deal more besides │\n│  it's all   confusing as hell right now │\n╰──╯",
			"[pasted text #1 · 4 lines: ╭─ rush … │ ╰──╯]"},
		{"\n\n", "[pasted text #1 · 1 line]"},
	} {
		got := PasteChip(1, c.text)
		if got != c.want {
			t.Errorf("PasteChip(%q) = %q, want %q", c.text, got, c.want)
		}
		if m := PasteChipRe.FindStringSubmatch(got); m == nil || m[1] != "1" || m[0] != got {
			t.Errorf("PasteChipRe doesn't take %q whole: %q", got, m)
		}
	}
	// Chips kept in old drafts still match, to expand on send.
	for _, old := range []string{"see [Pasted text #3 +9 lines] ok", "see [#3 4 lines: line1…line4] ok"} {
		if m := PasteChipRe.FindStringSubmatch(old); m == nil || m[1] != "3" || !HasPasteChip(old) {
			t.Errorf("an old chip should still match: %q", old)
		}
	}
}
