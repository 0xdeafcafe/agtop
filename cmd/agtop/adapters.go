package main

// The agents agtop knows: each registers itself with internal/agent.
import (
	_ "github.com/0xdeafcafe/agtop/internal/adapters/acp"
	_ "github.com/0xdeafcafe/agtop/internal/adapters/claude"
	_ "github.com/0xdeafcafe/agtop/internal/adapters/codex"
)
