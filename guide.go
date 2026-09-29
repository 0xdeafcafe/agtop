// Package rush holds what the rush program ships with from its own root.
package rush

import _ "embed"

// Guide is rush's README, for the agent #ask starts to answer from.
//
//go:embed README.md
var Guide string
