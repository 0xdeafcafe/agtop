package read

import (
	"io"
	"os"
)

func f(r io.Reader, transcriptPath, cfg string) {
	_, _ = io.ReadAll(r)                        // want `io.ReadAll reads however much there is`
	_, _ = io.ReadAll(io.LimitReader(r, 1<<20)) // bounded: fine
	_, _ = os.ReadFile(transcriptPath)          // want `os.ReadFile of a transcript`
	_, _ = os.ReadFile("/x/s.jsonl")            // want `os.ReadFile of a transcript`
	_, _ = os.ReadFile(cfg)                     // a small file: fine
}
