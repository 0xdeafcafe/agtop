package convo

import "testing"

func TestPasteChip(t *testing.T) {
	for _, c := range []struct{ text, want string }{
		{"line1\nline2\nline3\nline4", "[#1 4 lines: line1…line4]"},
		{"   \n\tfunc main() {\n\t\treturn [x]\n\t}\n", "[#1 4 lines: func main(…return x }]"},
		{"package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(1) }\n", "[#1 5 lines: package…intln(1) }]"},
		{"\n\n", "[#1 1 line]"},
	} {
		got := PasteChip(1, c.text)
		if got != c.want {
			t.Errorf("PasteChip(%q) = %q, want %q", c.text, got, c.want)
		}
		if m := PasteChipRe.FindStringSubmatch(got); m == nil || m[1] != "1" || m[0] != got {
			t.Errorf("PasteChipRe doesn't take %q whole: %q", got, m)
		}
	}
	if m := PasteChipRe.FindStringSubmatch("see [Pasted text #3 +9 lines] ok"); m == nil || m[1] != "3" {
		t.Error("an old chip should still match")
	}
}
