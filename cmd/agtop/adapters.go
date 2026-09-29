package main

// The agents agtop knows: each registers itself with internal/agent.
import (
	_ "github.com/0xdeafcafe/agtop/internal/adapters/acp"
	_ "github.com/0xdeafcafe/agtop/internal/adapters/claude"
	_ "github.com/0xdeafcafe/agtop/internal/adapters/codex"
	_ "github.com/0xdeafcafe/agtop/internal/adapters/copilot"
	_ "github.com/0xdeafcafe/agtop/internal/adapters/deepseek"
	_ "github.com/0xdeafcafe/agtop/internal/adapters/glm"
	_ "github.com/0xdeafcafe/agtop/internal/adapters/ollama"
	_ "github.com/0xdeafcafe/agtop/internal/adapters/pi"
)
