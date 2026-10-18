package terminal

import "errors"

var ErrCancelled = errors.New("cancelled")

type Choice struct {
	Title  string
	Detail string
}
