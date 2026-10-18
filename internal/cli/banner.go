package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/kitanovicd/blindspot/internal/terminal"
)

// The three rows share a 51-column canvas; block characters occupy one cell.
var wordmark = []string{
	"█▀▀▀▄ █     ▀█▀ █▄  █ █▀▀▀▄ ▄▀▀▀▀ █▀▀▀▄ ▄▀▀▀▄ ▀▀█▀▀",
	"█▀▀▀▄ █      █  █ ▀▄█ █   █  ▀▀▀▄ █▀▀▀  █   █   █  ",
	"▀▀▀▀  ▀▀▀▀▀ ▀▀▀ ▀   ▀ ▀▀▀▀  ▀▀▀▀  ▀      ▀▀▀    ▀  ",
}

func (a *app) banner(out io.Writer) {
	if a.bannerShown {
		return
	}
	a.bannerShown = true
	style := terminal.For(out)
	file, ok := out.(*os.File)
	if !ok || !terminal.IsTerminal(file) || os.Getenv("TERM") == "dumb" {
		fmt.Fprintf(out, "\n  %s  /  mutation coverage\n\n", style.Text(terminal.Heading, "BLINDSPOT"))
		return
	}
	width, height := terminal.Size(file)
	if width == 0 {
		width = 80
	}
	if height == 0 {
		height = 24
	}
	lines := wordmark
	if width < 55 || height < 18 {
		lines = []string{"B L I N D S P O T"}
		if width < 20 {
			lines = []string{"BLINDSPOT"}
		}
	}
	fmt.Fprintln(out)
	for _, line := range lines {
		padding := strings.Repeat(" ", max(0, (width-utf8.RuneCountInString(line))/2))
		fmt.Fprintln(out, padding+style.Text(terminal.Heading, strings.TrimRight(line, " ")))
	}
	const subtitle = "Mutation coverage for Foundry"
	if width >= len(subtitle)+4 {
		fmt.Fprintln(out)
		padding := strings.Repeat(" ", (width-len(subtitle))/2)
		fmt.Fprintln(out, padding+style.Text(terminal.Muted, subtitle))
	}
	fmt.Fprintln(out)
}
