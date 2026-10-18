package mutation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kitanovicd/blindspot/internal/report"
	"github.com/kitanovicd/blindspot/internal/storage"
	"github.com/kitanovicd/blindspot/internal/terminal"
	"github.com/kitanovicd/blindspot/internal/toolchain"
)

// Runner evaluates mutations in private project snapshots. Progress is written
// to the supplied writer; completed results are returned independently of CLI output.
type Runner struct {
	progress io.Writer
	style    terminal.Style
}

// NewRunner constructs a runner. A nil writer disables progress output.
func NewRunner(progress io.Writer) *Runner {
	if progress == nil {
		progress = io.Discard
	}
	return &Runner{progress: progress, style: terminal.For(progress)}
}

func (r *Runner) logf(format string, args ...any) {
	fmt.Fprintf(r.progress, "  "+format+"\n", args...)
}

func (r *Runner) section(title string) {
	fmt.Fprintf(r.progress, "\n  %s\n", r.style.Text(terminal.Heading, title))
}

// Run executes the baseline and selected mutations, persisting results along the
// way. Its directory result identifies the saved artifacts, including on failure.
func (r *Runner) Run(ctx context.Context, opts Options) (*report.Run, string, error) {
	if err := opts.Validate(); err != nil {
		return nil, "", err
	}
	root, err := ProjectRoot(opts.Project)
	if err != nil {
		return nil, "", err
	}
	unlock, err := storage.LockProject(root, false)
	if err != nil {
		return nil, "", err
	}
	defer unlock()
	if err := storage.Remember(root); err != nil {
		return nil, "", fmt.Errorf("prepare saved project: %w", err)
	}
	start := time.Now()
	id := start.UTC().Format("20060102-150405") + "-" + digest([]byte(fmt.Sprint(start.UnixNano())))[:6]
	store, err := storage.ProjectDir(root)
	if err != nil {
		return nil, "", err
	}
	runDir := filepath.Join(store, "runs", id)
	if err = os.MkdirAll(runDir, 0700); err != nil {
		return nil, "", err
	}
	if opts.InstallCommand != "" || opts.BuildCommand != "" || opts.TestCommand != "" {
		if err := r.prepareProject(ctx, root, filepath.Join(runDir, "logs", "setup"), opts); err != nil {
			return nil, runDir, err
		}
	}
	cfg, err := loadFoundry(ctx, root, filepath.Join(runDir, "logs", "config"), opts.Profile)
	if err != nil {
		return nil, runDir, err
	}
	if cfg.FFI && !opts.AllowFFI {
		return nil, runDir, fmt.Errorf("FFI is enabled in profile %q. Re-run with --allow-ffi to permit its subprocesses, or use a profile with ffi = false. FFI commands can access files and services outside the project copies", opts.Profile)
	}
	if cfg.FFI {
		r.logf("%s", r.style.Text(terminal.Yellow, "FFI      enabled (--allow-ffi); subprocesses are not sandboxed"))
	}
	gambit, err := toolchain.Locate(opts.Gambit, "gambit")
	if err != nil {
		return nil, runDir, err
	}
	solc, err := toolchain.ResolveSolc(opts.Solc, cfg.Solc, root)
	if err != nil {
		return nil, runDir, err
	}
	files, err := sourceFiles(root, cfg.Src)
	if err != nil {
		return nil, runDir, err
	}
	files, err = selectFiles(ctx, root, files, opts)
	if err != nil {
		return nil, runDir, err
	}
	ver, _ := exec.Command("forge", "--version").Output()
	run := &report.Run{
		SchemaVersion:    1,
		ID:               id,
		Project:          root,
		StartedAt:        start.UTC(),
		State:            "running",
		ManagedLifecycle: true,
		Seed:             opts.Seed,
		Limit:            opts.Limit,
		Scope:            strings.Join(files, ", "),
		Profile:          opts.Profile,
		ForgeVersion:     strings.TrimSpace(string(ver)),
		Gambit:           gambit,
		Solc:             solc,
		TimeoutSeconds:   opts.Timeout,
		InstallCommand:   opts.InstallCommand,
		BuildCommand:     opts.BuildCommand,
		TestCommand:      opts.TestCommand,
		Files:            map[string]string{},
		Mutations:        []report.Mutation{},
		Errors:           []string{},
	}
	run.Executable, _ = os.Executable()
	run.FFI = cfg.FFI
	run.BuildTimeoutSeconds = opts.buildTimeoutSeconds()
	finalize := func() error {
		run.DurationMS = time.Since(start).Milliseconds()
		run.Refresh()
		if err := saveRun(root, runDir, run); err != nil {
			return err
		}
		if !opts.KeepArtifacts && run.State != "running" {
			if err := storage.Prune(runDir, run, run.State != "completed"); err != nil {
				return fmt.Errorf("could not remove replay files: %w", err)
			}
			r.logf("Storage  Replay files removed; report and JSON kept")
		}
		return nil
	}
	r.logf("Project  %s", root)
	r.logf("Profile  %s · %d source file(s) · seed %d", opts.Profile, len(files), opts.Seed)
	r.section("Prepare")
	r.logf("  Copying project into an isolated snapshot…")
	snapshot := filepath.Join(runDir, "snapshot")
	home, err := storage.Home()
	if err != nil {
		return run, runDir, err
	}
	home, err = filepath.EvalSymlinks(home)
	if err != nil {
		return run, runDir, err
	}
	excludes := []string{cfg.Out, cfg.CachePath, ".blindspot-build", home}
	if err = copyTree(root, snapshot, excludes); err != nil {
		return run, runDir, err
	}
	if err = seedCorpus(cfg, root, snapshot); err != nil {
		return run, runDir, err
	}
	for _, file := range files {
		source, e := os.ReadFile(filepath.Join(snapshot, file))
		if e != nil {
			return run, runDir, e
		}
		run.Files[file] = string(source)
	}
	// Record only non-secret compiler configuration needed for local replay.
	replayCfg := cfg
	replayCfg.EthRPCURL = ""
	if err = writeJSON(filepath.Join(runDir, "compiler.json"), replayCfg); err != nil {
		return run, runDir, err
	}
	run.Baseline, err = r.runBaseline(ctx, snapshot, root, solc, cfg, opts, filepath.Join(runDir, "logs", "baseline"))
	if err != nil {
		run.State = "failed"
		if ctx.Err() != nil {
			run.State = "interrupted"
		}
		run.Errors = append(run.Errors, err.Error())
		return run, runDir, errors.Join(err, finalize())
	}
	mutants, generationErrors := r.generateSources(ctx, root, snapshot, runDir, files, gambit, solc, cfg, opts)
	run.Mutations = mutants
	run.Errors = append(run.Errors, generationErrors...)
	run.Candidates = len(mutants)
	// Stable seeded sampling across files; IDs identify content, not Gambit's renumbered IDs.
	if opts.Limit > 0 && len(run.Mutations) > opts.Limit {
		sort.Slice(run.Mutations, func(i, j int) bool {
			return digest([]byte(fmt.Sprint(opts.Seed)+run.Mutations[i].ID)) < digest([]byte(fmt.Sprint(opts.Seed)+run.Mutations[j].ID))
		})
		run.Mutations = run.Mutations[:opts.Limit]
	}
	sort.Slice(run.Mutations, func(i, j int) bool {
		a, b := run.Mutations[i], run.Mutations[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.ID < b.ID
	})
	if len(run.Mutations) == 0 {
		run.State = "incomplete"
		if len(run.Errors) == 0 {
			run.Errors = append(run.Errors, "No applicable mutations were generated in the selected scope.")
		}
		return run, runDir, errors.Join(errors.New("no mutations could be evaluated; inspect the report"), finalize())
	}
	if err = finalize(); err != nil {
		return run, runDir, err
	}
	workerCount := min(opts.Jobs, len(run.Mutations))
	r.section("Evaluate")
	r.logf("  %d mutations · %d workers · configured test suite", len(run.Mutations), workerCount)
	workers, err := prepareWorkers(snapshot, runDir, workerCount)
	if err != nil {
		run.State = "incomplete"
		run.Errors = append(run.Errors, err.Error())
		return run, runDir, errors.Join(err, finalize())
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	var mu sync.Mutex
	stopProgress := r.startEvaluationProgress(len(run.Mutations), func() report.Summary {
		mu.Lock()
		defer mu.Unlock()
		return run.Summary
	})
	for _, worker := range workers {
		wg.Add(1)
		go func(worker string) {
			defer wg.Done()
			for idx := range jobs {
				mu.Lock()
				original := run.Mutations[idx]
				mu.Unlock()
				m := testMutant(ctx, worker, root, runDir, solc, cfg, opts, original)
				mu.Lock()
				run.Mutations[idx] = m
				if e := finalize(); e != nil {
					run.Errors = append(run.Errors, "Could not checkpoint results: "+e.Error())
				}
				mu.Unlock()
			}
		}(worker)
	}
dispatch:
	for idx := range run.Mutations {
		select {
		case <-ctx.Done():
			break dispatch
		case jobs <- idx:
		}
	}
	close(jobs)
	wg.Wait()
	stopProgress()
	run.State = "completed"
	run.Refresh()
	if run.Summary.Caught+run.Summary.Survived == 0 && ctx.Err() == nil {
		run.Errors = append(run.Errors, "No valid mutants were evaluated. Invalid-only results do not establish mutation coverage.")
	}
	if ctx.Err() != nil {
		run.State = "interrupted"
	} else if len(run.Errors) > 0 || run.Summary.Timeout+run.Summary.Error+run.Summary.NotRun > 0 {
		run.State = "incomplete"
	}
	if err = finalize(); err != nil {
		return run, runDir, err
	}
	// Workers are disposable. Keep the original snapshot, mutants, and logs for replay.
	_ = os.RemoveAll(filepath.Join(runDir, "workers"))
	return run, runDir, nil
}
