//go:build !darwin && !linux

package terminal

import (
	"context"
	"fmt"
	"io"
	"os"
)

func Select(context.Context, *os.File, io.Writer, string, []Choice, bool) ([]int, error) {
	return nil, fmt.Errorf("arrow-key menus require macOS or Linux")
}
