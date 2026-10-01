package ui

import (
	"errors"
	"github.com/0xdeafcafe/rush/internal/agent"
	"strings"
	"testing"
)

func TestNativeAccountInfoIsVisible(t *testing.T) {
	m, _ := draftModel()
	info := agent.AccountSummary{SignedIn: true, Method: "native keychain", Name: "Work", Email: "person@example.test", Plan: "Pro", Note: "Local account information"}
	signInsMsg{kind: "vibe", summary: &info}.applyTo(m)
	var text strings.Builder
	for _, r := range m.nativeAccountRows("vibe") {
		text.WriteString(r.label + " " + r.value + " " + r.what + "\n")
	}
	for _, want := range []string{"credentials available", "Work", "person@example.test", "Pro", "native keychain", "Refresh account and models"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("account info missing %q", want)
		}
	}
}
func TestNativeAccountActionOffersSignIn(t *testing.T) {
	m, _ := draftModel()
	if _, ok := agent.As[agent.Authenticator]("vibe"); !ok {
		t.Fatal("Vibe native setup unavailable")
	}
	if cmd := m.addAccount("vibe"); cmd == nil {
		t.Fatal("account action did not offer native setup")
	}
	if strings.Contains(m.status, "can't keep more") {
		t.Fatal("old unsupported account warning returned")
	}
}

func TestNativeAccountUnknownIsNotSignedOut(t *testing.T) {
	for _, kind := range []string{"kimi", "vibe"} {
		m, _ := draftModel()
		info := agent.AccountSummary{Status: "unknown", Note: "Native credential storage unavailable"}
		signInsMsg{kind: kind, summary: &info, err: errors.New("local credentials unavailable")}.applyTo(m)
		if status := m.nativeAccountRows(agent.Kind(kind))[0].value; status != "unknown" {
			t.Fatalf("%s unavailable evidence: %s", kind, status)
		}
		// A key checker can find an item that the summary cannot read. Do not
		// replace that positive evidence with a false signed-out label.
		signInsMsg{kind: kind, summary: &info}.applyTo(m)
		if status := m.nativeAccountRows(agent.Kind(kind))[0].value; status != "credentials available" {
			t.Fatalf("%s discarded credential evidence: %s", kind, status)
		}
	}
}
