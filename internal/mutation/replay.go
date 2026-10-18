package mutation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/kitanovicd/blindspot/internal/report"
)

// ReplayOptions selects a saved mutant and optional timeout overrides in seconds.
type ReplayOptions struct {
	Project, RunID, MutationID string
	Timeout, BuildTimeout      int
}

// Replay evaluates one mutation against the original saved snapshot.
func (r *Runner) Replay(ctx context.Context, input ReplayOptions) (report.Mutation, error) {
	root, err := findProject(input.Project)
	if err != nil {
		return report.Mutation{}, err
	}
	run, dir, err := LoadRun(root, input.RunID)
	if err != nil {
		return report.Mutation{}, err
	}
	var selected *report.Mutation
	for i := range run.Mutations {
		if run.Mutations[i].ID == input.MutationID {
			selected = &run.Mutations[i]
			break
		}
	}
	if selected == nil {
		return report.Mutation{}, fmt.Errorf("mutation ID not found in run %s", run.ID)
	}
	var cfg foundryConfig
	data, err := os.ReadFile(filepath.Join(dir, "compiler.json"))
	if err != nil {
		return report.Mutation{}, err
	}
	if err = json.Unmarshal(data, &cfg); err != nil {
		return report.Mutation{}, err
	}
	if cfg.FFI != run.FFI {
		return report.Mutation{}, fmt.Errorf("saved FFI configuration does not match the run; start a new run with --allow-ffi if needed")
	}
	if run.FFI {
		r.logf("FFI      enabled (saved --allow-ffi run); subprocesses are not sandboxed")
	}
	worker := filepath.Join(dir, "replays", strconv.FormatInt(time.Now().UnixNano(), 10))
	if err = copyTree(filepath.Join(dir, "snapshot"), worker, nil); err != nil {
		return report.Mutation{}, err
	}
	defer os.RemoveAll(worker)
	seconds := run.TimeoutSeconds
	if input.Timeout > 0 {
		seconds = input.Timeout
	}
	r.logf("Replaying %s against the original saved snapshot", selected.ID)
	replayLogs := filepath.Join(dir, "replay-logs", filepath.Base(worker))
	opts := Options{Seed: run.Seed, Profile: run.Profile, Timeout: seconds, BuildTimeout: run.BuildTimeoutSeconds, replayLogs: replayLogs}
	opts.BuildCommand, opts.TestCommand = run.BuildCommand, run.TestCommand
	if input.BuildTimeout > 0 {
		opts.BuildTimeout = input.BuildTimeout
	} else if input.Timeout > 0 {
		opts.BuildTimeout = max(opts.BuildTimeout, opts.Timeout)
	}
	if _, err := r.runBaseline(ctx, worker, root, run.Solc, cfg, opts, filepath.Join(replayLogs, "baseline")); err != nil {
		return report.Mutation{}, err
	}
	m := testMutant(ctx, worker, root, dir, run.Solc, cfg, opts, *selected)

	return m, nil
}
