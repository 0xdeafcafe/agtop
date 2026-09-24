package menubar

import "testing"

// Reading what a host waits on only takes apart the lines that can say so.
func TestSkipped(t *testing.T) {
	for line, want := range map[string]bool{
		`{"type":"assistant","message":{}}`:                            true,
		`{"type":"user","message":{}}`:                                 true,
		`{"type":"stream_event","event":{}}`:                           true,
		`{"type":"control_request","request_id":"r1","request":{}}`:    false,
		`{"type":"control_cancel_request","request_id":"r1"}`:          false,
		`{"request_id":"r1","type":"agtop_answered"}`:                  false,
		`{"info":{"state":"blocked"},"type":"agtop_info"}`:             false,
		`{"agtop_sent":true,"message":{"content":"hi"},"type":"user"}`: false,
	} {
		if got := skipped([]byte(line)); got != want {
			t.Errorf("skipped(%s) = %v", line, got)
		}
	}
}
