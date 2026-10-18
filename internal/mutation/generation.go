package mutation

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kitanovicd/blindspot/internal/report"
	"github.com/kitanovicd/blindspot/internal/terminal"
)

// generateSources collects unique mutations while keeping progress output on
// the caller's goroutine. Each file gets a result and slow files get live updates.
func (r *Runner) generateSources(ctx context.Context, root, snapshot, runDir string, files []string, gambit, solc string, cfg foundryConfig, opts Options) ([]report.Mutation, []string) {
	started := time.Now()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	r.section("Generate")
	r.logf("  Finding mutations with Gambit in %s…", sourceFileCount(len(files)))
	type result struct {
		mutations []report.Mutation
		err       error
	}
	mutations := []report.Mutation{}
	var failures []string
	seen := map[string]bool{}
filesLoop:
	for i, file := range files {
		if ctx.Err() != nil {
			break
		}
		fmt.Fprintf(r.progress, "\n    %s %s\n", r.style.Text(terminal.Muted, fmt.Sprintf("[%d/%d]", i+1, len(files))), r.style.Text(terminal.Emphasis, file))
		fileStarted := time.Now()
		ticker.Reset(15 * time.Second)
		done := make(chan result, 1)
		go func() {
			mutants, err := generate(ctx, root, snapshot, runDir, file, gambit, solc, cfg, opts)
			done <- result{mutations: mutants, err: err}
		}()
		for {
			select {
			case generated := <-done:
				if generated.err != nil {
					failures = append(failures, generated.err.Error())
					if ctx.Err() == nil {
						r.logf("    %s", r.style.Text(terminal.Red, "Generation failed. See the report for details."))
					} else {
						r.logf("    %s", r.style.Text(terminal.Yellow, "Interrupted"))
					}
				} else {
					before := len(mutations)
					for _, m := range generated.mutations {
						if !seen[m.ID] {
							mutations = append(mutations, m)
							seen[m.ID] = true
						}
					}
					if found := len(mutations) - before; found > 0 {
						r.logf("    Mutations found: %s", r.style.Text(terminal.Accent, fmt.Sprint(found)))
					} else {
						r.logf("    %s", r.style.Text(terminal.Muted, "No applicable mutations"))
					}
				}
				continue filesLoop
			case <-ticker.C:
				r.logf("    %s", r.style.Text(terminal.Muted, fmt.Sprintf("Still generating · %s elapsed", time.Since(fileStarted).Round(time.Second))))
			}
		}
	}
	fmt.Fprintln(r.progress)
	r.logf("  Total mutations: %s", r.style.Text(terminal.Accent, fmt.Sprint(len(mutations))))
	r.logf("  %s", r.style.Text(terminal.Muted, fmt.Sprintf("Generation time: %s", time.Since(started).Round(time.Second))))
	if len(failures) > 0 {
		r.logf("  %s", r.style.Text(terminal.Red, "Could not process: "+sourceFileCount(len(failures))))
	}
	if ctx.Err() != nil {
		r.logf("  %s", r.style.Text(terminal.Yellow, "Generation interrupted"))
	}
	return mutations, failures
}

func sourceFileCount(n int) string {
	if n == 1 {
		return "1 source file"
	}
	return fmt.Sprintf("%d source files", n)
}

func generate(ctx context.Context, root, snapshot, runDir, file, gambit, solc string, cfg foundryConfig, opts Options) ([]report.Mutation, error) {
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
	var mutants []report.Mutation
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
		mutants = append(mutants, report.Mutation{ID: "m-" + digest([]byte(fingerprint))[:12], File: file, Line: line, Column: column, Operator: row[1], Original: row[4], Replacement: row[5], Diff: entry.Diff, Artifact: filepath.ToSlash(rel), Status: "not-run", Reason: "Queued", Failures: []report.TestResult{}})
	}
	return mutants, nil
}
