// Package terminal provides optional ANSI styling for human-readable CLI output.
package terminal

import (
	"fmt"
	"io"
	"os"
)

// Color uses the terminal's own palette so output follows the user's theme.
type Color string

const (
	Heading  Color = "1;36"
	Emphasis Color = "1"
	Cyan     Color = "36"
	Green    Color = "32"
	Yellow   Color = "33"
	Red      Color = "31"
	Muted    Color = "90"
)

// Style is bound to one output stream. Its zero value emits plain text.
type Style struct{ enabled bool }

// For enables color only on a terminal, unless NO_COLOR or TERM=dumb disables it.
func For(w io.Writer) Style {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled || os.Getenv("TERM") == "dumb" {
		return Style{}
	}
	f, ok := w.(*os.File)
	return Style{enabled: ok && isTerminal(f)}
}

// Text styles a display string without changing the underlying result data.
func (s Style) Text(color Color, text string) string {
	if !s.enabled || text == "" {
		return text
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
