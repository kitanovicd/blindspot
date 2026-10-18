package mutation

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/kitanovicd/blindspot/internal/report"
	"github.com/kitanovicd/blindspot/internal/terminal"
)

// Forge's JSON output is buffered, so report elapsed time independently of stdout.
func (r *Runner) progressStep(label string, limit time.Duration, invoke func() processResult) processResult {
	r.logf("  %s (limit %s)…", label, limit)
	started := time.Now()
	done := make(chan processResult, 1)
	go func() { done <- invoke() }()
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case result := <-done:
			return result
		case <-tick.C:
			r.logf("  %s · %s elapsed / %s limit", label, time.Since(started).Round(time.Second), limit)
		}
	}
}

func (r *Runner) runBaseline(ctx context.Context, worker, root, solc string, cfg foundryConfig, opts Options, logs string) (report.Baseline, error) {
	r.section("Baseline")
	b := report.Baseline{Phase: "build"}
	stop := func(status, reason string, result processResult) (report.Baseline, error) {
		b.Status, b.Reason, b.Log = status, reason, shortLog(combined(result))
		b.DurationMS = b.BuildDurationMS + b.TestDurationMS
		return b, fmt.Errorf("%s. See %s", strings.TrimRight(reason, ". "), filepath.Join(logs, b.Phase))
	}
	buildLimit := time.Duration(opts.buildTimeoutSeconds()) * time.Second
	build := r.progressStep("Compiling original project", buildLimit, func() processResult {
		return runForge(ctx, "build", worker, root, solc, cfg, opts, filepath.Join(logs, "build"))
	})
	b.BuildDurationMS = build.DurationMS
	if build.Cancelled {
		return stop("not-run", "Baseline compilation interrupted; tests have not run", build)
	}
	if build.Timeout {
		return stop("timeout", fmt.Sprintf("Baseline compilation timed out after %s; tests have not run. Retry with --build-timeout %d", buildLimit, opts.buildTimeoutSeconds()*2), build)
	}
	if build.Code != 0 || build.Err != nil {
		return stop("error", "Baseline compilation failed; tests have not run", build)
	}
	r.logf("  %s in %s", r.style.Text(terminal.Green, "Compilation finished"), time.Duration(build.DurationMS)*time.Millisecond)
	b.Phase = "test"
	testLimit := time.Duration(opts.Timeout) * time.Second
	result := r.progressStep("Running original tests", testLimit, func() processResult {
		return runForge(ctx, "test", worker, root, solc, cfg, opts, filepath.Join(logs, "test"))
	})
	b.TestDurationMS = result.DurationMS
	status, reason, tests := classify(result)
	b.Tests = len(tests)
	if status != "survived" {
		if status == "timeout" {
			reason = fmt.Sprintf("Baseline tests timed out after %s. Retry with --timeout %d", testLimit, opts.Timeout*2)
		} else if status == "caught" {
			reason = "original tests must pass before mutation testing: " + reason
		} else if status == "not-run" {
			reason = "Baseline tests interrupted"
		} else {
			reason = "Could not evaluate the baseline: " + reason
		}
		if status == "caught" {
			status = "failed"
		}
		return stop(status, reason, result)
	}
	b.Status = "passed"
	b.DurationMS = b.BuildDurationMS + b.TestDurationMS
	r.logf("  %s in %s", r.style.Count(terminal.Green, b.Tests, "tests passed"), time.Duration(b.TestDurationMS)*time.Millisecond)
	return b, nil
}
