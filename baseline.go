package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

func (o RunOptions) buildTimeoutSeconds() int {
	if o.BuildTimeout > 0 {
		return o.BuildTimeout
	}
	return max(600, o.Timeout)
}

// Forge's JSON output is buffered, so report elapsed time independently of stdout.
func executeProgress(label string, limit time.Duration, invoke func() ProcessResult) ProcessResult {
	logStep("%s (limit %s)…", label, limit)
	started := time.Now()
	done := make(chan ProcessResult, 1)
	go func() { done <- invoke() }()
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case result := <-done:
			return result
		case <-tick.C:
			logStep("%s · %s elapsed / %s limit", label, time.Since(started).Round(time.Second), limit)
		}
	}
}

func runBaseline(ctx context.Context, worker, root, solc string, cfg FoundryConfig, opts RunOptions, logs string) (Baseline, error) {
	b := Baseline{Phase: "build"}
	stop := func(status, reason string, result ProcessResult) (Baseline, error) {
		b.Status, b.Reason, b.Log = status, reason, shortLog(combined(result))
		b.DurationMS = b.BuildDurationMS + b.TestDurationMS
		return b, fmt.Errorf("%s. See %s", strings.TrimRight(reason, ". "), filepath.Join(logs, b.Phase))
	}
	args, err := forgeArgs("build", worker, root, solc, cfg, opts.Seed)
	if err != nil {
		return stop("error", err.Error(), ProcessResult{})
	}
	buildLimit := time.Duration(opts.buildTimeoutSeconds()) * time.Second
	build := executeProgress("Baseline  compiling original project", buildLimit, func() ProcessResult {
		return execute(ctx, "forge", args, worker, filepath.Join(logs, "build"), buildLimit, opts.Profile, workerEnv(cfg, root, worker))
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
	logStep("Baseline  compilation finished in %s", time.Duration(build.DurationMS)*time.Millisecond)
	b.Phase = "test"
	args, err = forgeArgs("test", worker, root, solc, cfg, opts.Seed)
	if err != nil {
		return stop("error", err.Error(), ProcessResult{})
	}
	testLimit := time.Duration(opts.Timeout) * time.Second
	result := executeProgress("Baseline  running original tests", testLimit, func() ProcessResult {
		return execute(ctx, "forge", args, worker, filepath.Join(logs, "test"), testLimit, opts.Profile, workerEnv(cfg, root, worker))
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
	logStep("Baseline  %d tests passed in %s", b.Tests, time.Duration(b.TestDurationMS)*time.Millisecond)
	return b, nil
}
