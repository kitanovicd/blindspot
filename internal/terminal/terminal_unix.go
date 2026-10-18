//go:build darwin || linux

package terminal

import (
	"os"
	"syscall"
	"unsafe"
)

func isTerminal(file *os.File) bool {
	conn, err := file.SyscallConn()
	if err != nil {
		return false
	}
	var state syscall.Termios
	var errno syscall.Errno
	err = conn.Control(func(fd uintptr) {
		_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, fd, readTermios, uintptr(unsafe.Pointer(&state)))
	})
	return err == nil && errno == 0
}

// Size returns the terminal's columns and rows, or zeros when unavailable.
func Size(file *os.File) (int, int) {
	conn, err := file.SyscallConn()
	if err != nil {
		return 0, 0
	}
	var window struct{ Rows, Cols, X, Y uint16 }
	var errno syscall.Errno
	err = conn.Control(func(fd uintptr) {
		_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&window)))
	})
	if err != nil || errno != 0 {
		return 0, 0
	}
	return int(window.Cols), int(window.Rows)
}
