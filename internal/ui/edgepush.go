package ui

import "time"

// edgePush counts presses of an arrow into the edge of a box with text in
// it: the third within a second asks to go past it, to the other pane,
// and a fourth (confirmation.again) says yes.
func (m *Model) edgePush(key string) bool {
	now := time.Now()
	if key != m.edgeKey || now.Sub(m.edgeAt) > time.Second {
		m.edgeN = 0
	}
	m.edgeKey, m.edgeAt = key, now
	m.edgeN++
	if m.edgeN < 3 {
		return false
	}
	m.edgeN = 0
	return true
}
