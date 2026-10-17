package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type RunOptions struct {
	Project, Gambit, Solc, Profile string
	Targets                        []string
	Limit, Jobs, Timeout, Line     int
	BuildTimeout                   int
	Seed                           uint64
	Open, FailOnSurvivors          bool
	AllowFFI                       bool
	Changed                        string
	ReplayLogs                     string
}

func logStep(format string, args ...any) { fmt.Fprintf(os.Stderr, "  "+format+"\n", args...) }
func combined(r ProcessResult) string {
	s := r.Stdout
	if r.Stderr != "" {
		s += "\n" + r.Stderr
	}
	if r.Err != nil {
		s += "\n" + r.Err.Error()
	}
	return s
}
func shortLog(text string) string {
	if len(text) > 24000 {
		return text[len(text)-24000:]
	}
	return text
}

func selectFiles(ctx context.Context, root string, files []string, opts RunOptions) ([]string, error) {
	selected := files
	if len(opts.Targets) > 0 {
		selected = nil
		for _, file := range files {
			for _, target := range opts.Targets {
				if filepath.IsAbs(target) {
					rel, err := filepath.Rel(root, target)
					if err != nil {
						return nil, err
					}
					target = rel
				}
				target = strings.TrimSuffix(filepath.ToSlash(filepath.Clean(target)), "/")
				match, _ := filepath.Match(target, file)
				if file == target || strings.HasPrefix(file, target+"/") || match {
					selected = append(selected, file)
					break
				}
			}
		}
	}
	if opts.Changed != "" {
		// NUL separated paths preserve spaces; include staged, unstaged, and untracked source files.
		cmd := exec.CommandContext(ctx, "git", "diff", "--name-only", "-z", opts.Changed, "--")
		cmd.Dir = root
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("cannot compare Git ref %q: %w", opts.Changed, err)
		}
		cmd = exec.CommandContext(ctx, "git", "ls-files", "--others", "--exclude-standard", "-z")
		cmd.Dir = root
		extra, err := cmd.Output()
		if err != nil {
			return nil, err
		}
		changed := map[string]bool{}
		for _, p := range strings.Split(string(out)+string(extra), "\x00") {
			changed[p] = true
		}
		var narrowed []string
		for _, f := range selected {
			if changed[f] {
				narrowed = append(narrowed, f)
			}
		}
		selected = narrowed
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no Solidity source files matched. Targets are relative to the project root; tests and scripts are excluded")
	}
	return selected, nil
}

func generate(ctx context.Context, root, snapshot, runDir, file, gambit, solc string, cfg FoundryConfig, opts RunOptions) ([]Mutation, error) {
	source, err := os.ReadFile(filepath.Join(snapshot, file))
	if err != nil {
		return nil, err
	}
	// Gambit 1.0.6 panics when a source has no newline at all. A trailing
	// newline in the private generation input preserves every original offset.
	if !strings.Contains(string(source), "\n") {
		if err = os.WriteFile(filepath.Join(snapshot, file), append(append([]byte{}, source...), '\n'), 0600); err != nil {
			return nil, err
		}
		defer os.WriteFile(filepath.Join(snapshot, file), source, 0600)
	}
	genDir := filepath.Join(runDir, "generated", digest([]byte(file))[:12])
	if err := os.MkdirAll(genDir, 0700); err != nil {
		return nil, err
	}
	maps, err := remappings(cfg, root, snapshot)
	if err != nil {
		return nil, err
	}
	// Gambit expects remapping entries without trailing slashes.
	for i, m := range maps {
		a, b, _ := strings.Cut(m, "=")
		// Gambit resolves JSON paths relative to the JSON file, not its process cwd.
		if !filepath.IsAbs(b) {
			b = filepath.Join(snapshot, b)
		}
		maps[i] = strings.TrimSuffix(a, "/") + "=" + strings.TrimSuffix(b, "/")
	}
	settings := map[string]any{"filename": filepath.Join(snapshot, file), "sourceroot": snapshot, "outdir": filepath.Join(genDir, "out"), "solc": solc, "solc_remappings": maps, "seed": opts.Seed, "skip_validate": true, "solc_optimize": cfg.Optimizer}
	if cfg.EVMVersion != "" {
		settings["solc_evm_version"] = cfg.EVMVersion
	}
	if opts.Limit > 0 && opts.Line == 0 {
		settings["num_mutants"] = opts.Limit
	}
	conf := filepath.Join(genDir, "gambit.json")
	if err = writeJSON(conf, settings); err != nil {
		return nil, err
	}
	result := execute(ctx, gambit, []string{"mutate", "--json", conf}, snapshot, filepath.Join(genDir, "logs"), time.Duration(max(120, opts.Timeout))*time.Second, opts.Profile, nil)
	if result.Code != 0 || result.Timeout || result.Cancelled {
		return nil, fmt.Errorf("Gambit could not generate mutations for %s: %s", file, shortLog(combined(result)))
	}
	data, err := os.ReadFile(filepath.Join(genDir, "out", "gambit_results.json"))
	if err != nil {
		return nil, err
	}
	var entries []struct{ ID, Name, Description, Diff, Original string }
	if err = json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	if !exists(filepath.Join(genDir, "out", "input_json", filepath.Base(file)+"_json.ast.json")) {
		return nil, fmt.Errorf("Gambit did not produce a source AST for %s (it can exit successfully after a compiler error): %s", file, shortLog(combined(result)))
	}
	log, err := os.Open(filepath.Join(genDir, "out", "mutants.log"))
	if err != nil {
		return nil, err
	}
	defer log.Close()
	rows, err := csv.NewReader(log).ReadAll()
	if err != nil {
		return nil, err
	}
	byID := map[string][]string{}
	for _, row := range rows {
		if len(row) >= 6 {
			byID[row[0]] = row
		}
	}
	var mutants []Mutation
	for _, entry := range entries {
		row, ok := byID[entry.ID]
		if !ok {
			return nil, fmt.Errorf("Gambit result %s has no source location", entry.ID)
		}
		lineText, colText, ok := strings.Cut(row[3], ":")
		line, e := strconv.Atoi(lineText)
		column, e2 := strconv.Atoi(colText)
		if !ok || e != nil || e2 != nil || line < 1 || column < 1 {
			return nil, fmt.Errorf("invalid Gambit location: %q", row[3])
		}
		if opts.Line > 0 && line != opts.Line {
			continue
		}
		if filepath.ToSlash(entry.Original) != file {
			return nil, fmt.Errorf("Gambit source mismatch: %s != %s", entry.Original, file)
		}
		artifact := filepath.Join(genDir, "out", entry.Name)
		if !inside(genDir, artifact) || !exists(artifact) {
			return nil, fmt.Errorf("invalid Gambit mutant artifact: %s", entry.Name)
		}
		rel, _ := filepath.Rel(runDir, artifact)
		fingerprint := fmt.Sprintf("%s\x00%s\x00%d:%d\x00%s\x00%s\x00%s", file, digest(source), line, column, row[1], row[4], row[5])
		mutants = append(mutants, Mutation{ID: "m-" + digest([]byte(fingerprint))[:12], File: file, Line: line, Column: column, Operator: row[1], Original: row[4], Replacement: row[5], Diff: entry.Diff, Artifact: filepath.ToSlash(rel), Status: "not-run", Reason: "Queued", Failures: []TestResult{}})
	}
	return mutants, nil
}

func testMutant(ctx context.Context, worker, root, runDir, solc string, cfg FoundryConfig, opts RunOptions, mutant Mutation) Mutation {
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
	if opts.ReplayLogs != "" {
		logs = filepath.Join(opts.ReplayLogs, m.ID)
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

func runMutation(ctx context.Context, opts RunOptions) (*Run, string, error) {
	root, err := findProject(opts.Project)
	if err != nil {
		return nil, "", err
	}
	start := time.Now()
	id := start.UTC().Format("20060102-150405") + "-" + digest([]byte(fmt.Sprint(start.UnixNano())))[:6]
	runDir := filepath.Join(root, ".blindspot", "runs", id)
	if err = os.MkdirAll(runDir, 0700); err != nil {
		return nil, "", err
	}
	cfg, err := loadFoundry(ctx, root, filepath.Join(runDir, "logs", "config"), opts.Profile)
	if err != nil {
		return nil, runDir, err
	}
	if cfg.FFI && !opts.AllowFFI {
		return nil, runDir, fmt.Errorf("FFI is enabled in profile %q. Re-run with --allow-ffi to permit its subprocesses, or use a profile with ffi = false. FFI commands can access files and services outside the project copies", opts.Profile)
	}
	if cfg.FFI {
		logStep("FFI      enabled (--allow-ffi); subprocesses are not sandboxed")
	}
	gambit, err := locateTool(opts.Gambit, "gambit")
	if err != nil {
		return nil, runDir, err
	}
	solc, err := resolveSolc(opts.Solc, cfg, root)
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
	run := &Run{SchemaVersion: 1, ID: id, Project: root, StartedAt: start.UTC(), State: "running", Seed: opts.Seed, Limit: opts.Limit, Scope: strings.Join(files, ", "), Profile: opts.Profile, ForgeVersion: strings.TrimSpace(string(ver)), Gambit: gambit, Solc: solc, TimeoutSeconds: opts.Timeout, Files: map[string]string{}, Mutations: []Mutation{}, Errors: []string{}}
	run.Executable, _ = os.Executable()
	run.FFI = cfg.FFI
	run.BuildTimeoutSeconds = opts.buildTimeoutSeconds()
	finalize := func() error {
		run.DurationMS = time.Since(start).Milliseconds()
		run.Refresh()
		return saveRun(root, runDir, run)
	}
	logStep("Project  %s", root)
	logStep("Profile  %s · %d source file(s) · seed %d", opts.Profile, len(files), opts.Seed)
	logStep("Copying project into an isolated snapshot…")
	snapshot := filepath.Join(runDir, "snapshot")
	excludes := []string{cfg.Out, cfg.CachePath, ".blindspot-build"}
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
	run.Baseline, err = runBaseline(ctx, snapshot, root, solc, cfg, opts, filepath.Join(runDir, "logs", "baseline"))
	if err != nil {
		run.State = "failed"
		if ctx.Err() != nil {
			run.State = "interrupted"
		}
		run.Errors = append(run.Errors, err.Error())
		_ = finalize()
		return run, runDir, err
	}
	for i, file := range files {
		if ctx.Err() != nil {
			break
		}
		logStep("Generate  [%d/%d] %s", i+1, len(files), file)
		mutants, e := generate(ctx, root, snapshot, runDir, file, gambit, solc, cfg, opts)
		if e != nil {
			run.Errors = append(run.Errors, e.Error())
			logStep("Generation failed for %s; details will be in the report", file)
			continue
		}
		run.Mutations = append(run.Mutations, mutants...)
	}
	// Stable seeded sampling across files; IDs identify content, not Gambit's renumbered IDs.
	seen := map[string]bool{}
	unique := []Mutation{}
	for _, m := range run.Mutations {
		if !seen[m.ID] {
			unique = append(unique, m)
			seen[m.ID] = true
		}
	}
	run.Mutations = unique
	run.Candidates = len(unique)
	if opts.Limit > 0 && len(unique) > opts.Limit {
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
		_ = finalize()
		return run, runDir, errors.New("no mutations could be evaluated; inspect the report")
	}
	if err = finalize(); err != nil {
		return run, runDir, err
	}
	logStep("Evaluate  %d mutants · %d workers · full test suite", len(run.Mutations), opts.Jobs)
	jobs := make(chan int)
	var wg sync.WaitGroup
	var mu sync.Mutex
	completed := 0
	for n := 0; n < min(opts.Jobs, len(run.Mutations)); n++ {
		worker := filepath.Join(runDir, "workers", fmt.Sprint(n))
		if err = copyTree(snapshot, worker, nil); err != nil {
			return run, runDir, err
		}
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
				completed++
				run.Refresh()
				logStep("[%d/%d] %-8s %s:%d  %s → %s", completed, len(run.Mutations), m.Status, m.File, m.Line, strings.TrimSpace(m.Original), strings.TrimSpace(m.Replacement))
				if e := finalize(); e != nil {
					run.Errors = append(run.Errors, "Could not checkpoint results: "+e.Error())
				}
				mu.Unlock()
			}
		}(worker)
	}
	for idx := range run.Mutations {
		if ctx.Err() != nil {
			break
		}
		jobs <- idx
	}
	close(jobs)
	wg.Wait()
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

func saveRun(root, dir string, run *Run) error {
	if err := writeJSON(filepath.Join(dir, "run.json"), run); err != nil {
		return err
	}
	if err := writeReport(filepath.Join(dir, "report.html"), run); err != nil {
		return err
	}
	return writeJSON(filepath.Join(root, ".blindspot", "latest.json"), map[string]string{"id": run.ID})
}
