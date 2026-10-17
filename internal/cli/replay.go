package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/kitanovicd/blindspot/internal/mutation"
)

func (a *app) replay(ctx context.Context, args []string) int {
	f := a.flags("replay")
	project := f.String("project", ".", "Foundry project")
	id := f.String("run", "", "Run ID")
	timeout := f.Int("timeout", 0, "Override saved timeout")
	buildTimeout := f.Int("build-timeout", 0, "Override saved compilation timeout")
	if err := parseFlags(f, args); err != nil {
		return flagExit(err)
	}
	if f.NArg() != 1 {
		return a.fail(fmt.Errorf("usage: blindspot replay MUTATION_ID [--project PATH] [--run RUN_ID]"))
	}
	if *timeout < 0 || *buildTimeout < 0 {
		return a.fail(fmt.Errorf("timeout overrides must be nonnegative"))
	}
	m, err := a.runner.Replay(ctx, mutation.ReplayOptions{
		Project:      *project,
		RunID:        *id,
		MutationID:   f.Arg(0),
		Timeout:      *timeout,
		BuildTimeout: *buildTimeout,
	})
	if err != nil {
		return a.fail(err)
	}
	fmt.Fprintf(a.out, "\n  %s  %s:%d\n  %s → %s\n  %s\n", a.outStyle.Text(statusColor(m.Status), strings.ToUpper(m.Status)), m.File, m.Line, m.Original, m.Replacement, m.Reason)
	if m.Status == "error" || m.Status == "timeout" || m.Status == "not-run" {
		return 3
	}
	return 0
}
