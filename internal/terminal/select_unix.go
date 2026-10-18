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
	draw := func() {
		if drawn > 0 {
			fmt.Fprintf(output, "\x1b[%dA", drawn)
		}
		var window struct{ Rows, Cols, X, Y uint16 }
		syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&window)))
		width, rows := int(window.Cols), int(window.Rows)
		if width < 20 {
			width = 80
		}
		if rows < 8 {
			rows = 24
		}
		count := min(len(choices), min(8, rows-6))
		first := max(0, min(selected-count/2, len(choices)-count))
		lines := []string{"", "  " + title}
		help := "  ↑/↓ move · Enter select · Esc cancel"
		if multi {
			help = "  ↑/↓ move · Space select · A select all · Enter continue · Esc cancel"
		}
		lines = append(lines, help, "")
		for i := first; i < first+count; i++ {
			prefix := "  "
			if i == selected {
				prefix = "> "
			}
			if multi {
				if marked[i] {
					prefix += "[x] "
				} else {
					prefix += "[ ] "
				}
			}
			lines = append(lines, "  "+prefix+choices[i].Title)
		}
		lines = append(lines, "  "+choices[selected].Detail)
		for _, line := range lines {
			fmt.Fprintf(output, "\r\x1b[2K%s\n", clip(line, width-1))
		}
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
