// Package rush holds what the rush program ships with from its own root.
package rush

import _ "embed"

// Guide is rush's full guide (docs/guide.md), for the agent #ask starts to answer from.
//
//go:embed docs/guide.md
var Guide string
