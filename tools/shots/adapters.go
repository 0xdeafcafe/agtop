package main

// The agents rush knows, as cmd/rush has them: each registers itself.
import (
	_ "github.com/0xdeafcafe/rush/internal/adapters/acp"
	_ "github.com/0xdeafcafe/rush/internal/adapters/claude"
	_ "github.com/0xdeafcafe/rush/internal/adapters/codex"
	_ "github.com/0xdeafcafe/rush/internal/adapters/copilot"
	_ "github.com/0xdeafcafe/rush/internal/adapters/deepseek"
	_ "github.com/0xdeafcafe/rush/internal/adapters/glm"
	_ "github.com/0xdeafcafe/rush/internal/adapters/ollama"
	_ "github.com/0xdeafcafe/rush/internal/adapters/pi"
)
