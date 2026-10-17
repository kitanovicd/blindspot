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
