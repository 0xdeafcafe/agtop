package convo

import (
	"testing"

	"github.com/0xdeafcafe/rush/internal/host"
)

func TestLimitChange(t *testing.T) {
	lim := &host.Limit{}
	for _, c := range []struct {
		was, now host.Info
		want     string
	}{
		{host.Info{}, host.Info{ID: "a", Limit: lim}, ""},
		{host.Info{ID: "a", Limit: lim}, host.Info{ID: "a", Limit: lim}, ""},
		{host.Info{ID: "a", Limit: lim}, host.Info{ID: "a"}, "limit lifted · carries on"},
		{host.Info{ID: "a", Account: "x", Limit: lim}, host.Info{ID: "a", Account: "y"}, "now on y · was x"},
	} {
		if got := limitChange(c.was, c.now); got != c.want {
			t.Errorf("limitChange(%+v, %+v) = %q, want %q", c.was, c.now, got, c.want)
		}
	}
}
