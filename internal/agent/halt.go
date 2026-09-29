package agent

import "time"

// Halt is a turn an agent ended on an error instead of an answer: the API
// was out of reach, or a usage limit was hit. Nothing more happens until
// someone says go on.
type Halt struct {
	Kind string    `json:"k"` // the agent's word for the error: rate_limit, server_error…
	Text string    `json:"t"` // what it told the user, first line
	At   time.Time `json:"a"`
}
