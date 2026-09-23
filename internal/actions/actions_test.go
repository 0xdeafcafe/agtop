package actions

import (
	"reflect"
	"testing"
)

func TestRespawnFlagsDropSessionIdentity(t *testing.T) {
	got := respawnFlags([]string{"--agent", "claude", "--resume", "abc", "--model", "opus[1m]", "--session-id", "x", "--fork-session"})
	want := []string{"--agent", "claude", "--model", "opus[1m]"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
}

func TestRespawnFlagsDropAllResumeSpellings(t *testing.T) {
	got := respawnFlags([]string{"--resume=abc", "-c", "--model", "opus", "--continue", "--session-id=x"})
	if !reflect.DeepEqual(got, []string{"--model", "opus"}) {
		t.Fatalf("%v", got)
	}
}

func TestNewIDPrefersTheAttachHint(t *testing.T) {
	out := "Resumed bc572d22-5b3b\n  claude attach 5606143a    open in this terminal\n"
	if got := newID(out); got != "5606143a" {
		t.Fatal(got)
	}
}
