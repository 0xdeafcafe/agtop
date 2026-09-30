package main

// The agents rush knows, as cmd/rush has them: each registers itself.
import (
	_ "github.com/0xdeafcafe/rush/internal/adapters/acp"
	_ "github.com/0xdeafcafe/rush/internal/adapters/claude"
	_ "github.com/0xdeafcafe/rush/internal/adapters/codex"
	_ "github.com/0xdeafcafe/rush/internal/adapters/copilot"
	_ "github.com/0xdeafcafe/rush/internal/adapters/deepseek"
	_ "github.com/0xdeafcafe/rush/internal/adapters/gemini"
	_ "github.com/0xdeafcafe/rush/internal/adapters/glm"
	_ "github.com/0xdeafcafe/rush/internal/adapters/ollama"
	_ "github.com/0xdeafcafe/rush/internal/adapters/pi"
	_ "github.com/0xdeafcafe/rush/internal/adapters/vibe"

	// And the plugins that come with it, as cmd/rush has them.
	_ "github.com/0xdeafcafe/rush/internal/bundled/clean"
	_ "github.com/0xdeafcafe/rush/internal/bundled/drafts"
	_ "github.com/0xdeafcafe/rush/internal/bundled/queue"
)
