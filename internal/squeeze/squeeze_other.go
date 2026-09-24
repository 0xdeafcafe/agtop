//go:build !darwin

package squeeze

import (
	"errors"
	"os"
)

func Compressed(os.FileInfo) bool    { return false }
func diskUse(info os.FileInfo) int64 { return info.Size() }
func File(string) error              { return errors.New("only on macOS") }
