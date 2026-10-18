//go:build !darwin && !linux

package terminal

import "os"

func isTerminal(*os.File) bool { return false }

// Size returns zeros on platforms without terminal support.
func Size(*os.File) (int, int) { return 0, 0 }
