package report

import (
	"os/exec"
	"runtime"
)

// Open asks the operating system to display a report file or local URL.
func Open(target string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", target)
	} else if runtime.GOOS == "windows" {
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	} else {
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Run()
}
