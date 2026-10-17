package mutation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kitanovicd/blindspot/internal/report"
)

func testMutant(ctx context.Context, worker, root, runDir, solc string, cfg foundryConfig, opts Options, mutant report.Mutation) report.Mutation {
	start := time.Now()
	m := mutant
	if ctx.Err() != nil {
		m.Reason = "Run interrupted"
		return m
	}
	original, err := os.ReadFile(filepath.Join(worker, m.File))
	if err != nil {
		m.Status = "error"
		m.Reason = err.Error()
		return m
	}
	mutated, err := os.ReadFile(filepath.Join(runDir, m.Artifact))
	if err != nil {
		m.Status = "error"
		m.Reason = err.Error()
		return m
	}
	if err = os.WriteFile(filepath.Join(worker, m.File), mutated, 0600); err != nil {
		m.Status = "error"
		m.Reason = err.Error()
		return m
	}
	defer os.WriteFile(filepath.Join(worker, m.File), original, 0600)
	logs := filepath.Join(runDir, "logs", m.ID)
	if opts.replayLogs != "" {
		logs = filepath.Join(opts.replayLogs, m.ID)
	}
	if err = resetCorpus(filepath.Join(runDir, "snapshot"), worker); err != nil {
		m.Status = "error"
		m.Reason = err.Error()
		return m
	}
	buildArgs, err := forgeArgs("build", worker, root, solc, cfg, opts.Seed)
	if err != nil {
		m.Status = "error"
		m.Reason = err.Error()
		return m
	}
	build := execute(ctx, "forge", buildArgs, worker, filepath.Join(logs, "build"), time.Duration(opts.buildTimeoutSeconds())*time.Second, opts.Profile, workerEnv(cfg, root, worker))
	if build.Code != 0 || build.Timeout || build.Cancelled {
		m.Status = "error"
		m.Reason = "Compilation process failed"
		m.Log = shortLog(combined(build))
		if build.Cancelled {
			m.Status = "not-run"
			m.Reason = "Run interrupted"
		} else if build.Timeout {
			m.Status = "timeout"
			m.Reason = fmt.Sprintf("Compilation exceeded %ds; retry with --build-timeout %d", opts.buildTimeoutSeconds(), opts.buildTimeoutSeconds()*2)
		} else if strings.Contains(m.Log, "Compiler run failed") || strings.Contains(m.Log, "Compiler error") {
			m.Status = "invalid"
			m.Reason = "Mutant does not compile under the Foundry configuration"
		}
		m.DurationMS = time.Since(start).Milliseconds()
		return m
	}
	args, err := forgeArgs("test", worker, root, solc, cfg, opts.Seed)
	if err != nil {
		m.Status = "error"
		m.Reason = err.Error()
		return m
	}
	result := execute(ctx, "forge", args, worker, filepath.Join(logs, "test"), time.Duration(opts.Timeout)*time.Second, opts.Profile, workerEnv(cfg, root, worker))
	status, reason, tests := classify(result)
	m.Status = status
	m.Reason = reason
	m.TestsRun = len(tests)
	for _, t := range tests {
		if t.Status == "Failure" {
			m.Failures = append(m.Failures, t)
		}
	}
	sort.Slice(m.Failures, func(i, j int) bool { return m.Failures[i].Name < m.Failures[j].Name })
	if status == "error" || status == "timeout" {
		m.Log = shortLog(combined(result))
	}
	m.DurationMS = time.Since(start).Milliseconds()
	return m
}
