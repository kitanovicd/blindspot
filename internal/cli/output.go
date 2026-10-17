package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/kitanovicd/blindspot/internal/report"
	"github.com/kitanovicd/blindspot/internal/terminal"
)

func (a *app) printSummary(r *report.Run, dir string) {
	s := r.Summary
	fmt.Fprintf(a.out, "\n  %s · %s\n", a.outStyle.Text(statusColor(r.State), strings.ToUpper(r.State)), time.Duration(r.DurationMS)*time.Millisecond)
	if r.Baseline.Status != "passed" {
		fmt.Fprintln(a.out, "  No mutation coverage evaluated; baseline did not complete successfully.")
		if r.Baseline.Reason != "" {
			fmt.Fprintln(a.out, "  "+r.Baseline.Reason)
		}
		fmt.Fprintf(a.out, "\n  Report  %s\n  JSON    %s\n\n", filepath.Join(dir, "report.html"), filepath.Join(dir, "run.json"))
		return
	}
	fmt.Fprintf(a.out, "  %s · %s\n",
		a.outStyle.Count(terminal.Yellow, s.GapLines, "lines with gaps"),
		a.outStyle.Count(terminal.Green, s.CaughtLines, "lines with all evaluated mutations caught"))
	fmt.Fprintf(a.out, "  %s · %s · %s · %s\n",
		a.outStyle.Count(terminal.Green, s.Caught, "caught"),
		a.outStyle.Count(terminal.Yellow, s.Survived, "survived"),
		a.outStyle.Count(terminal.Muted, s.Invalid, "invalid"),
		a.outStyle.Count(terminal.Yellow, s.Timeout+s.Error+s.NotRun, "unresolved"))
	if r.Limit > 0 {
		fmt.Fprintf(a.out, "  Sampled scope: up to %d mutants. Unassessed lines are not marked covered.\n", r.Limit)
	}
	fmt.Fprintf(a.out, "\n  Report  %s\n  JSON    %s\n\n", filepath.Join(dir, "report.html"), filepath.Join(dir, "run.json"))
}

func statusColor(status string) terminal.Color {
	switch status {
	case "passed", "caught", "completed":
		return terminal.Green
	case "failed", "error":
		return terminal.Red
	case "survived", "timeout", "not-run", "interrupted", "incomplete":
		return terminal.Yellow
	case "invalid":
		return terminal.Muted
	default:
		return terminal.Cyan
	}
}
