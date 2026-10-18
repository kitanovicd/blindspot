// Package terminal provides optional ANSI styling for human-readable CLI output.
package terminal

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Color holds ANSI styles. The accent is light blue (#87afff); result colors use the terminal's palette.
type Color string

const (
	Heading  Color = "1;38;5;111"
	Selected Color = Heading
	Emphasis Color = "1"
	Accent   Color = "38;5;111"
	Green    Color = "32"
	Yellow   Color = "33"
	Red      Color = "31"
	Muted    Color = "90"
)

// Style is bound to one output stream. Its zero value emits plain text.
type Style struct {
	enabled  bool
	extended bool
}

// IsTerminal reports whether a stream supports interactive terminal input.
func IsTerminal(file *os.File) bool { return isTerminal(file) }

// For enables color only on a terminal, unless NO_COLOR or TERM=dumb disables it.
func For(w io.Writer) Style {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled || os.Getenv("TERM") == "dumb" {
		return Style{}
	}
	f, ok := w.(*os.File)
	colorTerm := os.Getenv("COLORTERM")
	return Style{
		enabled:  ok && isTerminal(f),
		extended: strings.Contains(os.Getenv("TERM"), "256color") || colorTerm == "truecolor" || colorTerm == "24bit",
	}
}

// Text styles a display string without changing the underlying result data.
func (s Style) Text(color Color, text string) string {
	if !s.enabled || text == "" {
		return text
	}
	if !s.extended {
		switch color {
		case Heading:
			color = "1;34"
		case Accent:
			color = "34"
		}
	}
	return "\x1b[" + string(color) + "m" + text + "\x1b[0m"
}

// Count keeps zero counts neutral so only present outcomes attract attention.
func (s Style) Count(color Color, count int, label string) string {
	if count == 0 {
		color = Muted
	}
	return s.Text(color, fmt.Sprintf("%d %s", count, label))
}
