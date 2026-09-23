package host

import "testing"

func TestForkGetsItsOwnID(t *testing.T) {
	plain := Config{SessionID: "0123456789abcdef-aaaa"}
	plain.fillIDs()
	fork := Config{SessionID: plain.SessionID, Fork: true}
	fork.fillIDs()
	if fork.ID == plain.ID || fork.ID == "" || fork.SessionID != plain.SessionID {
		t.Fatalf("plain %q fork %q", plain.ID, fork.ID)
	}
}
