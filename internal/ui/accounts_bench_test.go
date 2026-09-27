package ui

import "testing"

// BenchmarkAccounts is what the top bar and Accounts ask of the accounts
// each frame.
func BenchmarkAccounts(b *testing.B) {
	m, _ := benchModel(200, 60)
	b.Run("rows", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			m.accountRows()
		}
	})
	b.Run("header", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			m.header()
		}
	})
}
