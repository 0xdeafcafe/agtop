package ui

import (
	"os"
	"os/exec"
	"time"

	tea "charm.land/bubbletea/v2"
)

type Model struct{ path string }

type other struct{}

func (m *Model) View() string {
	b, _ := os.ReadFile(m.path)         // want `os.ReadFile blocks agtop's UI`
	time.Sleep(time.Millisecond)        // want `time.Sleep blocks agtop's UI`
	_ = exec.Command("git").Run()       // want `exec.Run blocks agtop's UI`
	func() { _, _ = os.Stat(m.path) }() // want `os.Stat blocks agtop's UI`
	return string(b)
}

func (m Model) load() tea.Cmd {
	return func() tea.Msg {
		b, _ := os.ReadFile(m.path) // a tea.Cmd: off the UI goroutine
		return b
	}
}

func (m *Model) save() {
	go func() { _ = os.WriteFile(m.path, nil, 0o600) }()
}

func (other) fine() { _, _ = os.ReadFile("x") } // not the Model

func helper() { _, _ = os.ReadFile("x") } // not a method

func cmdErr(what string, fn func() error) tea.Cmd { return func() tea.Msg { return fn() } }

func (m *Model) handed() tea.Cmd {
	return cmdErr("x", func() error { time.Sleep(time.Millisecond); return nil }) // runs in the Cmd
}

func (m *Model) deferred() {
	defer func() { _ = os.Remove(m.path) }() // want `os.Remove blocks agtop's UI`
}
