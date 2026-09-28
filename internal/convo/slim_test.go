package convo

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/0xdeafcafe/agtop/internal/agent/tool"
	"github.com/0xdeafcafe/agtop/internal/headless"
)

func TestSlimResult(t *testing.T) {
	big := strings.Repeat("x", 4<<10)
	edit, _ := json.Marshal(map[string]any{"filePath": "a.go", "originalFile": big,
		"structuredPatch": []map[string]any{{"oldStart": 1, "oldLines": 1, "newStart": 1, "newLines": 1, "lines": []string{"-a", "+b"}}}})
	got := slimResult(tool.Edit, edit)
	if strings.Contains(string(got), big) || len(headless.Patches(got)) != 1 {
		t.Fatalf("edit kept the file or lost its patch: %.200s", got)
	}
	read, _ := json.Marshal(map[string]any{"type": "text", "file": map[string]any{"filePath": "a.go", "content": big, "numLines": 3, "startLine": 1, "totalLines": 9}})
	got = slimResult(tool.Read, read)
	var r struct {
		File struct {
			NumLines, TotalLines int
			Content              string
		}
	}
	if json.Unmarshal(got, &r) != nil || r.File.Content != "" || r.File.NumLines != 3 || r.File.TotalLines != 9 {
		t.Fatalf("read: %.200s", got)
	}
	bash, _ := json.Marshal(map[string]any{"stdout": big})
	if got := slimResult(tool.Shell, bash); string(got) != string(bash) {
		t.Fatal("a command's output is kept")
	}
}
