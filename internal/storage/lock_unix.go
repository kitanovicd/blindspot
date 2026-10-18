//go:build darwin || linux

package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// LockProject coordinates runs and replay (shared) with cleanup (exclusive).
// Kernel locks are released even if the process is killed.
func LockProject(project string, exclusive bool) (func(), error) {
	if err := checkStore(project, true); err != nil {
		return nil, err
	}
	fd, err := syscall.Open(filepath.Join(project, ".blindspot", ".lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "blindspot lock")
	mode := syscall.LOCK_SH
	if exclusive {
		mode = syscall.LOCK_EX
	}
	if err := syscall.Flock(fd, mode|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("project is busy with a run, replay, or cleanup: %s", project)
	}
	return func() { _ = syscall.Flock(fd, syscall.LOCK_UN); _ = file.Close() }, nil
}
