package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/"+module+"/@v/v0.2.0.info" {
			w.Write([]byte(`{"Version":"v0.2.0","Time":"2026-09-01T00:00:00Z"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	t.Setenv("GOPROXY", srv.URL+",direct")

	at := func(s string) time.Time { t, _ := time.Parse(time.RFC3339, s); return t }
	latest := Info{Version: "v0.0.0-20260924103914-d3b737083d4a", Time: at("2026-09-24T10:39:14Z")}
	ctx := context.Background()
	for _, c := range []struct {
		cur  Info
		want bool
	}{
		{latest, false},
		{Info{Version: latest.Version + "+dirty", Time: latest.Time}, false},
		{Info{Version: "v0.0.0-20260920000000-aaaaaaaaaaaa", Time: at("2026-09-20T00:00:00Z")}, true},
		{Info{Version: "v0.0.0-20260930000000-bbbbbbbbbbbb", Time: at("2026-09-30T00:00:00Z")}, false},
		{Info{Version: "v0.2.0"}, true},  // its time is asked for
		{Info{Version: "v9.9.9"}, false}, // unknown to the proxy: no answer, no nag
	} {
		if got := Newer(ctx, c.cur, latest); got != c.want {
			t.Errorf("Newer(%s) = %v, want %v", c.cur.Version, got, c.want)
		}
	}
	if s := latest.Short(); s[:7] != "d3b7370" {
		t.Errorf("Short() = %q", s)
	}
}
