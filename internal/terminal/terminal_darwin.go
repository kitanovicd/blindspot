package terminal

import "syscall"

const readTermios = syscall.TIOCGETA
const writeTermios = syscall.TIOCSETA
