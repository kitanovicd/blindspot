package terminal

import "syscall"

const readTermios = syscall.TCGETS
const writeTermios = syscall.TCSETS
