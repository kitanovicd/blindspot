//go:build !darwin && !linux

package mutation

import (
	"os/exec"
)

func configureProcess(cmd *exec.Cmd) {}
