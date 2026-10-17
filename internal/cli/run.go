package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/kitanovicd/blindspot/internal/mutation"
	"github.com/kitanovicd/blindspot/internal/report"
	"github.com/kitanovicd/blindspot/internal/terminal"
)

func (a *app) run(ctx context.Context, args []string) int {
	f := a.flags("run")
	var opts mutation.Options
	var all, jsonOut, open, failOnSurvivors bool
	f.StringVar(&opts.Project, "project", ".", "Foundry project directory")
	f.StringVar(&opts.Gambit, "gambit", "", "Gambit executable")
	f.StringVar(&opts.Solc, "solc", "", "Native solc executable")
	f.StringVar(&opts.Profile, "profile", defaultProfile(), "Foundry profile")
	f.IntVar(&opts.Limit, "limit", 50, "Maximum mutants; 0 for all")
	f.IntVar(&opts.Jobs, "jobs", min(2, runtime.NumCPU()), "Parallel workers")
	f.IntVar(&opts.Timeout, "timeout", 120, "Test execution timeout in seconds")
	f.IntVar(&opts.BuildTimeout, "build-timeout", 0, "Compilation timeout; 0 uses max(600, --timeout)")
	f.IntVar(&opts.Line, "line", 0, "Original line number")
	f.Uint64Var(&opts.Seed, "seed", 42, "Mutation and fuzz seed")
	f.BoolVar(&all, "all", false, "All mutations")
	f.BoolVar(&open, "open", false, "Open report")
	f.BoolVar(&opts.AllowFFI, "allow-ffi", false, "Permit configured FFI subprocesses outside the project copies")
	f.BoolVar(&failOnSurvivors, "fail-on-survivors", false, "Fail CI if mutations survive")
	f.BoolVar(&jsonOut, "json", false, "JSON results")
	f.StringVar(&opts.Changed, "changed", "", "Git ref for changed files")
	if err := parseFlags(f, args); err != nil {
		return flagExit(err)
	}
	opts.Targets = f.Args()
	if all {
		opts.Limit = 0
	}
	if err := opts.Validate(); err != nil {
		return a.fail(err)
	}
	fmt.Fprintf(a.errOut, "\n  %s  /  mutation coverage\n\n", a.errStyle.Text(terminal.Heading, "BLINDSPOT"))
	run, dir, err := a.runner.Run(ctx, opts)
	if run != nil {
		if jsonOut {
			if err := a.writeJSON(run); err != nil {
				return a.fail(err)
			}
		} else {
			a.printSummary(run, dir)
		}
		if open && reportExists(dir) {
			_ = report.Open(filepath.Join(dir, "report.html"))
		}
	}
	if ctx.Err() != nil {
		return 130
	}
	if err != nil {
		return a.fail(err)
	}
	if run.State != "completed" {
		return 3
	}
	if failOnSurvivors && run.Summary.Survived > 0 {
		return 2
	}
	return 0
}

func (a *app) writeJSON(run *report.Run) error {
	enc := json.NewEncoder(a.out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(run); err != nil {
		return fmt.Errorf("write JSON results: %w", err)
	}
	return nil
}

func reportExists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "report.html"))
	return err == nil
}
