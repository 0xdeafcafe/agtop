//go:build !darwin

package fleet

// dirUsage is what's inside a folder.
func dirUsage(dir string) int64 { return statUsage(dir) }
