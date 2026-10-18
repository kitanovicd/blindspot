//go:build darwin || linux

package terminal

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"unicode"
	"unsafe"
)

// Select uses a small inline menu, restoring terminal settings on every exit.
// Timed reads let cancellation work without leaving a reader goroutine behind.
func Select(ctx context.Context, input *os.File, output io.Writer, title string, choices []Choice, multi bool) ([]int, error) {
	if len(choices) == 0 {
		return nil, fmt.Errorf("no options available")
	}
	fd := input.Fd()
	var original syscall.Termios
	if _, _, err := syscall.Syscall(syscall.SYS_IOCTL, fd, readTermios, uintptr(unsafe.Pointer(&original))); err != 0 {
		return nil, err
	}
	state := original
	state.Lflag &^= syscall.ICANON | syscall.ECHO | syscall.ISIG
	state.Iflag &^= syscall.IXON
	state.Cc[syscall.VMIN], state.Cc[syscall.VTIME] = 0, 1
	if _, _, err := syscall.Syscall(syscall.SYS_IOCTL, fd, writeTermios, uintptr(unsafe.Pointer(&state))); err != 0 {
		return nil, err
	}
	defer func() {
		syscall.Syscall(syscall.SYS_IOCTL, fd, writeTermios, uintptr(unsafe.Pointer(&original)))
	}()
	fmt.Fprint(output, "\x1b[?25l")
	defer fmt.Fprint(output, "\x1b[?25h")
	selected, drawn := 0, 0
	marked := make([]bool, len(choices))
	style := For(output)
	clear := func() {
		if drawn == 0 {
			return
		}
		var frame strings.Builder
		fmt.Fprintf(&frame, "\x1b[%dA\r", drawn)
		for range drawn {
			frame.WriteString("\x1b[2K\n")
		}
		fmt.Fprintf(&frame, "\x1b[%dA\r", drawn)
		fmt.Fprint(output, frame.String())
		drawn = 0
	}
	defer clear()
	draw := func() {
		var window struct{ Rows, Cols, X, Y uint16 }
		syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&window)))
		width, rows := int(window.Cols), int(window.Rows)
		if width < 20 {
			width = 80
		}
		if rows < 8 {
			rows = 24
		}
		help := selectionHelp(multi, width-3)
		detail := choices[selected].Detail
		overhead := 4 + len(help)
		if detail != "" {
			overhead += 2
		}
		count := min(len(choices), min(8, max(1, rows-overhead-1)))
		first := max(0, min(selected-count/2, len(choices)-count))
		type line struct {
			text  string
			color Color
		}
		lines := []line{{}, {"  " + title, Heading}, {}}
		for i := first; i < first+count; i++ {
			prefix := "  "
			var color Color
			if multi && marked[i] {
				color = Green
			}
			if i == selected {
				prefix = "> "
				color = Selected
			}
			if multi {
				if marked[i] {
					prefix += "[x] "
				} else {
					prefix += "[ ] "
				}
			}
			lines = append(lines, line{"  " + prefix + choices[i].Title, color})
		}
		if detail != "" {
			lines = append(lines, line{}, line{"    " + detail, Muted})
		}
		lines = append(lines, line{})
		for _, hint := range help {
			lines = append(lines, line{"  " + hint, Muted})
		}
		var frame strings.Builder
		if drawn > 0 {
			fmt.Fprintf(&frame, "\x1b[%dA", drawn)
		}
		for _, row := range lines {
			text := clip(row.text, width-1)
			if row.color != "" {
				text = style.Text(row.color, text)
			}
			fmt.Fprintf(&frame, "\r\x1b[2K%s\n", text)
		}
		if extra := drawn - len(lines); extra > 0 {
			for range extra {
				frame.WriteString("\r\x1b[2K\n")
			}
			fmt.Fprintf(&frame, "\x1b[%dA", extra)
		}
		fmt.Fprint(output, frame.String())
		drawn = len(lines)
	}
	draw()
	escape := 0
	var buffer [1]byte
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		n, err := syscall.Read(int(fd), buffer[:])
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return nil, err
		}
		if n == 0 {
			if escape != 0 {
				return nil, ErrCancelled
			}
			continue
		}
		key := buffer[0]
		if escape == 1 && (key == '[' || key == 'O') {
			escape = 2
			continue
		}
		if escape == 2 {
			escape = 0
			if key == 'A' {
				selected = (selected + len(choices) - 1) % len(choices)
			}
			if key == 'B' {
				selected = (selected + 1) % len(choices)
			}
			draw()
			continue
		}
		if escape == 1 {
			return nil, ErrCancelled
		}
		switch key {
		case 27:
			escape = 1
			continue
		case 3, 4, 'q':
			return nil, ErrCancelled
		case 'k':
			selected = (selected + len(choices) - 1) % len(choices)
		case 'j':
			selected = (selected + 1) % len(choices)
		case ' ':
			if multi {
				marked[selected] = !marked[selected]
			}
		case 'a', 'A':
			if multi {
				all := true
				for _, value := range marked {
					all = all && value
				}
				for i := range marked {
					marked[i] = !all
				}
			}
		case '\r', '\n':
			if !multi {
				return []int{selected}, nil
			}
			var result []int
			for i, value := range marked {
				if value {
					result = append(result, i)
				}
			}
			return result, nil
		}
		draw()
	}
}

// Keep each shortcut together when the terminal needs more than one help line.
func selectionHelp(multi bool, width int) []string {
	hints := []string{"↑/↓ move", "Enter select", "Esc cancel"}
	if multi {
		hints = []string{"↑/↓ move", "Space toggle", "A toggle all", "Enter confirm", "Esc cancel"}
	}
	lines := []string{hints[0]}
	for _, hint := range hints[1:] {
		last := len(lines) - 1
		joined := lines[last] + " · " + hint
		if clip(joined, width) == joined {
			lines[last] = joined
		} else {
			lines = append(lines, hint)
		}
	}
	return lines
}

func clip(text string, width int) string {
	var output strings.Builder
	used := 0
	for _, r := range text {
		if unicode.IsControl(r) {
			r = ' '
		}
		size := 1
		if r >= 0x1100 {
			size = 2
		}
		if unicode.Is(unicode.Mn, r) {
			size = 0
		}
		if used+size > width {
			output.WriteString("…")
			break
		}
		output.WriteRune(r)
		used += size
	}
	return output.String()
}
