package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/kitanovicd/blindspot/internal/mutation"
	"github.com/kitanovicd/blindspot/internal/toolchain"
)

const version = "0.1.0"

type app struct {
	out, errOut io.Writer
	runner      *mutation.Runner
}

// Run executes one CLI invocation and returns its process exit code.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	a := &app{out: stdout, errOut: stderr, runner: mutation.NewRunner(stderr)}
	if len(args) == 0 {
		return a.text(help)
	}
	switch args[0] {
	case "help", "--help", "-h":
		return a.text(help)
	case "version", "--version":
		return a.text("blindspot " + version + " (Go · Gambit v1.0.6)\n")
	case "run":
		return a.run(ctx, args[1:])
	case "report":
		return a.report(ctx, args[1:])
	case "replay":
		return a.replay(ctx, args[1:])
	case "doctor":
		return a.doctor(ctx, args[1:])
	case "setup":
		if len(args) != 1 {
			return a.fail(fmt.Errorf("usage: blindspot setup"))
		}
		if err := toolchain.InstallGambit(ctx, a.out, a.errOut); err != nil {
			return a.fail(err)
		}
		return 0
	default:
		return a.fail(fmt.Errorf("unknown command %q. Run `blindspot help`", args[0]))
	}
}

func (a *app) fail(err error) int { fmt.Fprintln(a.errOut, "\n  Error:", err); return 1 }
func (a *app) text(text string) int {
	if _, err := io.WriteString(a.out, text); err != nil {
		return a.fail(fmt.Errorf("write output: %w", err))
	}
	return 0
}
