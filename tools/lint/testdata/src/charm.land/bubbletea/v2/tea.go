package tea

import "example.com/uv"

// Msg is an alias, as in the real bubbletea.
type Msg = uv.Event

type Cmd func() Msg
