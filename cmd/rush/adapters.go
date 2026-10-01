package main

// The agents rush knows: each registers itself with internal/agent.
import (
	_ "github.com/0xdeafcafe/rush/internal/adapters/acp"
	_ "github.com/0xdeafcafe/rush/internal/adapters/antigravity"
	_ "github.com/0xdeafcafe/rush/internal/adapters/claude"
	_ "github.com/0xdeafcafe/rush/internal/adapters/codex"
	_ "github.com/0xdeafcafe/rush/internal/adapters/copilot"
	_ "github.com/0xdeafcafe/rush/internal/adapters/cross"
	_ "github.com/0xdeafcafe/rush/internal/adapters/deepseek"
	_ "github.com/0xdeafcafe/rush/internal/adapters/gemini"
	_ "github.com/0xdeafcafe/rush/internal/adapters/glm"
	_ "github.com/0xdeafcafe/rush/internal/adapters/ollama"
	_ "github.com/0xdeafcafe/rush/internal/adapters/pi"
	_ "github.com/0xdeafcafe/rush/internal/adapters/vibe"
)
