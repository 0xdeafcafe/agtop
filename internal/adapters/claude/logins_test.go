package claude

import (
	"testing"
	"time"
)

func TestPutBackNeedsTwoLooks(t *testing.T) {
	if putBack("x", "y") {
		t.Fatal("put right on the first look: it may be a sign-in under way")
	}
	mismatch.at = time.Now().Add(-time.Minute)
	if !putBack("x", "y") {
		t.Fatal("not put right on the second look")
	}
	if putBack("x", "z") {
		t.Fatal("another mismatch starts over")
	}
}
