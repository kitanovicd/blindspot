package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/kitanovicd/blindspot/internal/report"
)

func (a *app) printSummary(r *report.Run, dir string) {
	s := r.Summary
	fmt.Fprintf(a.out, "\n  %s · %s\n", strings.ToUpper(r.State), time.Duration(r.DurationMS)*time.Millisecond)
	if r.Baseline.Status != "passed" {
		fmt.Fprintln(a.out, "  No mutation coverage evaluated; baseline did not complete successfully.")
		if r.Baseline.Reason != "" {
			fmt.Fprintln(a.out, "  "+r.Baseline.Reason)
		}
		fmt.Fprintf(a.out, "\n  Report  %s\n  JSON    %s\n\n", filepath.Join(dir, "report.html"), filepath.Join(dir, "run.json"))
		return
	}
	fmt.Fprintf(a.out, "  %d lines with gaps · %d lines with all evaluated mutations caught\n", s.GapLines, s.CaughtLines)
	fmt.Fprintf(a.out, "  %d caught · %d survived · %d invalid · %d unresolved\n", s.Caught, s.Survived, s.Invalid, s.Timeout+s.Error+s.NotRun)
	if r.Limit > 0 {
		fmt.Fprintf(a.out, "  Sampled scope: up to %d mutants. Unassessed lines are not marked covered.\n", r.Limit)
	}
	fmt.Fprintf(a.out, "\n  Report  %s\n  JSON    %s\n\n", filepath.Join(dir, "report.html"), filepath.Join(dir, "run.json"))
}
