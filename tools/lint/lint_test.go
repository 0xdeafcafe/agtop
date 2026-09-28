package lint

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestJSON(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), JSON, "j", "example.com/internal/jsonx")
}

func TestUIBlock(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), UIBlock, "example.com/internal/ui")
}

func TestHot(t *testing.T) { analysistest.Run(t, analysistest.TestData(), Hot, "hot") }

func TestRead(t *testing.T) { analysistest.Run(t, analysistest.TestData(), Read, "read") }
