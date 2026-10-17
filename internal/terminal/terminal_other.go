//go:build !darwin && !linux

package terminal

import "os"

func isTerminal(*os.File) bool { return false }
