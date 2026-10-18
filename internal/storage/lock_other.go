//go:build !darwin && !linux

package storage

import "fmt"

func LockProject(string, bool) (func(), error) {
	return nil, fmt.Errorf("project locking requires macOS or Linux; use WSL on Windows")
}
