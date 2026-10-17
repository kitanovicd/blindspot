package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const help = `
  blindspot — mutation coverage for Foundry

  Run your tests against Gambit mutations. See the gaps at the source.

  Commands
    run [files/directories]    Evaluate mutations and save a browser report
    report                     Open the latest report (no server required)
    replay <mutation-id>       Re-test one mutation from the saved snapshot
    doctor                     Check Foundry, Gambit, and compiler setup
    setup                      Install the pinned Gambit release locally
    version                    Print the version

  Examples
    blindspot run
    blindspot run src/Vault.sol --limit 30 --open
    blindspot run src/Vault.sol --line 16 --all
    blindspot run --changed origin/main --jobs 2
    blindspot report --serve
    blindspot replay m-0123456789ab

  Run options
    --project PATH             Foundry project directory (default: current)
    --limit N                  Maximum mutants to evaluate (default: 50)
    --all                      Evaluate all generated mutations in scope
    --line N                   Mutate one original line in the selected file
    --jobs N                   Parallel workers (default: 2, bounded by CPUs)
    --timeout SECONDS          Test execution limit (default: 120)
    --build-timeout SECONDS    Compilation limit (default: max of 600 and --timeout)
    --seed N                   Recorded mutation and fuzz seed (default: 42)
    --profile NAME             Foundry profile (default: FOUNDRY_PROFILE or default)
    --allow-ffi                 Permit FFI subprocesses when the profile enables them
    --solc PATH                Native solc executable; otherwise auto-resolved
    --gambit PATH              Gambit executable; otherwise auto-resolved
    --changed REF              Mutate source files changed since this Git ref
    --open                     Open the completed HTML report
    --fail-on-survivors         Exit 2 if any mutation survives (for CI)
    --json                     Write final results as JSON to stdout

  Exit codes: 0 complete · 1 setup/baseline failure · 2 survivors (opt-in)
              3 incomplete evaluation · 130 interrupted

  Reports stay in PROJECT/.blindspot/. Source files are never replaced in-place.
  Full docs and limitations: README.md
`

func normalizeArgs(args []string, bools map[string]bool) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			key := strings.TrimLeft(a, "-")
			key, _, hasValue := strings.Cut(key, "=")
			if !hasValue && !bools[key] && key != "help" && key != "h" && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		} else {
			positional = append(positional, a)
		}
	}
	return append(flags, positional...)
}

func defaultProfile() string {
	if v := os.Getenv("FOUNDRY_PROFILE"); v != "" {
		return v
	}
	return "default"
}
func newFlags(name string) *flag.FlagSet {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(os.Stderr)
	return f
}

func cli(ctx context.Context, args []string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Print(help)
		return 0
	}
	if args[0] == "version" || args[0] == "--version" {
		fmt.Println("blindspot " + Version + " (Go · Gambit v1.0.6)")
		return 0
	}
	fail := func(err error) int { fmt.Fprintln(os.Stderr, "\n  Error:", err); return 1 }
	switch args[0] {
	case "run":
		f := newFlags("run")
		var opts RunOptions
		var all, jsonOut bool
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
		f.BoolVar(&opts.Open, "open", false, "Open report")
		f.BoolVar(&opts.AllowFFI, "allow-ffi", false, "Permit configured FFI subprocesses outside the project copies")
		f.BoolVar(&opts.FailOnSurvivors, "fail-on-survivors", false, "Fail CI if mutations survive")
		f.BoolVar(&jsonOut, "json", false, "JSON results")
		f.StringVar(&opts.Changed, "changed", "", "Git ref for changed files")
		if err := f.Parse(normalizeArgs(args[1:], map[string]bool{"all": true, "open": true, "allow-ffi": true, "fail-on-survivors": true, "json": true})); err != nil {
			if err == flag.ErrHelp {
				return 0
			}
			return 1
		}
		opts.Targets = f.Args()
		if all {
			opts.Limit = 0
		}
		if opts.Jobs < 1 || opts.Jobs > 64 || opts.Timeout < 1 || opts.BuildTimeout < 0 || opts.Limit < 0 || opts.Line < 0 {
			return fail(fmt.Errorf("jobs must be 1–64, timeout positive, build-timeout, limit and line nonnegative"))
		}
		if opts.Line > 0 && len(opts.Targets) != 1 {
			return fail(fmt.Errorf("--line requires exactly one source file target"))
		}
		fmt.Fprint(os.Stderr, "\n  BLINDSPOT  /  mutation coverage\n\n")
		run, dir, err := runMutation(ctx, opts)
		if run != nil {
			if jsonOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				_ = enc.Encode(run)
			} else {
				printSummary(run, dir)
			}
			if opts.Open && exists(filepath.Join(dir, "report.html")) {
				_ = openBrowser(filepath.Join(dir, "report.html"))
			}
		}
		if ctx.Err() != nil {
			return 130
		}
		if err != nil {
			return fail(err)
		}
		if run.State != "completed" {
			return 3
		}
		if opts.FailOnSurvivors && run.Summary.Survived > 0 {
			return 2
		}
		return 0
	case "report":
		f := newFlags("report")
		project := f.String("project", ".", "Foundry project")
		id := f.String("run", "", "Run ID (default: latest)")
		serve := f.Bool("serve", false, "Serve on localhost")
		port := f.Int("port", 0, "Local port (default: automatic)")
		noOpen := f.Bool("no-open", false, "Do not open browser")
		export := f.String("export", "", "Export self-contained HTML")
		if err := f.Parse(normalizeArgs(args[1:], map[string]bool{"serve": true, "no-open": true})); err != nil {
			if err == flag.ErrHelp {
				return 0
			}
			return 1
		}
		root, err := findProject(*project)
		if err != nil {
			return fail(err)
		}
		run, dir, err := loadRun(root, *id)
		if err != nil {
			return fail(err)
		}
		file := filepath.Join(dir, "report.html")
		run.Refresh()
		run.Executable, _ = os.Executable()
		if err = writeReport(file, run); err != nil {
			return fail(err)
		}
		if *export != "" {
			data, err := os.ReadFile(file)
			if err != nil {
				return fail(err)
			}
			if err = os.WriteFile(*export, data, 0600); err != nil {
				return fail(err)
			}
			fmt.Println("Report exported to", *export)
		}
		if *serve {
			if err = serveReport(ctx, file, *port, !*noOpen); err != nil {
				return fail(err)
			}
		} else if !*noOpen {
			if err = openBrowser(file); err != nil {
				return fail(err)
			}
		} else {
			fmt.Println(file)
		}
		return 0
	case "replay":
		f := newFlags("replay")
		project := f.String("project", ".", "Foundry project")
		id := f.String("run", "", "Run ID")
		timeout := f.Int("timeout", 0, "Override saved timeout")
		buildTimeout := f.Int("build-timeout", 0, "Override saved compilation timeout")
		if err := f.Parse(normalizeArgs(args[1:], nil)); err != nil {
			if err == flag.ErrHelp {
				return 0
			}
			return 1
		}
		if f.NArg() != 1 {
			return fail(fmt.Errorf("usage: blindspot replay MUTATION_ID [--project PATH] [--run RUN_ID]"))
		}
		if *timeout < 0 || *buildTimeout < 0 {
			return fail(fmt.Errorf("timeout overrides must be nonnegative"))
		}
		root, err := findProject(*project)
		if err != nil {
			return fail(err)
		}
		run, dir, err := loadRun(root, *id)
		if err != nil {
			return fail(err)
		}
		var selected *Mutation
		for i := range run.Mutations {
			if run.Mutations[i].ID == f.Arg(0) {
				selected = &run.Mutations[i]
				break
			}
		}
		if selected == nil {
			return fail(fmt.Errorf("mutation ID not found in run %s", run.ID))
		}
		var cfg FoundryConfig
		data, err := os.ReadFile(filepath.Join(dir, "compiler.json"))
		if err != nil {
			return fail(err)
		}
		if err = json.Unmarshal(data, &cfg); err != nil {
			return fail(err)
		}
		if cfg.FFI != run.FFI {
			return fail(fmt.Errorf("saved FFI configuration does not match the run; start a new run with --allow-ffi if needed"))
		}
		if run.FFI {
			logStep("FFI      enabled (saved --allow-ffi run); subprocesses are not sandboxed")
		}
		worker := filepath.Join(dir, "replays", strconv.FormatInt(time.Now().UnixNano(), 10))
		if err = copyTree(filepath.Join(dir, "snapshot"), worker, nil); err != nil {
			return fail(err)
		}
		defer os.RemoveAll(worker)
		seconds := run.TimeoutSeconds
		if *timeout > 0 {
			seconds = *timeout
		}
		logStep("Replaying %s against the original saved snapshot", selected.ID)
		replayLogs := filepath.Join(dir, "replay-logs", filepath.Base(worker))
		opts := RunOptions{Seed: run.Seed, Profile: run.Profile, Timeout: seconds, BuildTimeout: run.BuildTimeoutSeconds, ReplayLogs: replayLogs}
		if *buildTimeout > 0 {
			opts.BuildTimeout = *buildTimeout
		} else if *timeout > 0 {
			opts.BuildTimeout = max(opts.BuildTimeout, opts.Timeout)
		}
		if _, err := runBaseline(ctx, worker, root, run.Solc, cfg, opts, filepath.Join(replayLogs, "baseline")); err != nil {
			return fail(err)
		}
		m := testMutant(ctx, worker, root, dir, run.Solc, cfg, opts, *selected)
		fmt.Printf("\n  %s  %s:%d\n  %s → %s\n  %s\n", strings.ToUpper(m.Status), m.File, m.Line, m.Original, m.Replacement, m.Reason)
		if m.Status == "error" || m.Status == "timeout" || m.Status == "not-run" {
			return 3
		}
		return 0
	case "doctor":
		f := newFlags("doctor")
		project := f.String("project", ".", "Foundry project")
		solc := f.String("solc", "", "Compiler path")
		gambit := f.String("gambit", "", "Gambit path")
		profile := f.String("profile", defaultProfile(), "Foundry profile")
		if err := f.Parse(args[1:]); err != nil {
			if err == flag.ErrHelp {
				return 0
			}
			return 1
		}
		root, err := findProject(*project)
		if err != nil {
			return fail(err)
		}
		cfg, err := loadFoundry(ctx, root, filepath.Join(root, ".blindspot", "doctor"), *profile)
		if err != nil {
			return fail(err)
		}
		g, e1 := locateTool(*gambit, "gambit")
		s, e2 := resolveSolc(*solc, cfg, root)
		fmt.Println("\n  Foundry configuration: OK\n  Project:", root, "\n  Profile:", *profile)
		if e1 != nil {
			fmt.Println("  Gambit:", e1)
		} else {
			fmt.Println("  Gambit:", g)
		}
		if e2 != nil {
			fmt.Println("  Solidity:", e2)
		} else {
			fmt.Println("  Solidity:", s)
		}
		if cfg.FFI {
			fmt.Println("  FFI: enabled; add --allow-ffi to your run command (subprocesses are not sandboxed)")
		} else {
			fmt.Println("  FFI: disabled")
		}
		fmt.Println("  Isolation: project snapshots\n  Runtime: Go binary + Foundry + Gambit + native solc")
		if e1 != nil || e2 != nil {
			return 1
		}
		return 0
	case "setup":
		if err := installGambit(ctx); err != nil {
			return fail(err)
		}
		return 0
	default:
		return fail(fmt.Errorf("unknown command %q. Run `blindspot help`", args[0]))
	}
}

func printSummary(r *Run, dir string) {
	s := r.Summary
	fmt.Printf("\n  %s · %s\n", strings.ToUpper(r.State), time.Duration(r.DurationMS)*time.Millisecond)
	if r.Baseline.Status != "passed" {
		fmt.Println("  No mutation coverage evaluated; baseline did not complete successfully.")
		if r.Baseline.Reason != "" {
			fmt.Println("  " + r.Baseline.Reason)
		}
		fmt.Printf("\n  Report  %s\n  JSON    %s\n\n", filepath.Join(dir, "report.html"), filepath.Join(dir, "run.json"))
		return
	}
	fmt.Printf("  %d lines with gaps · %d lines with all evaluated mutations caught\n", s.GapLines, s.CaughtLines)
	fmt.Printf("  %d caught · %d survived · %d invalid · %d unresolved\n", s.Caught, s.Survived, s.Invalid, s.Timeout+s.Error+s.NotRun)
	if r.Limit > 0 {
		fmt.Printf("  Sampled scope: up to %d mutants. Unassessed lines are not marked covered.\n", r.Limit)
	}
	fmt.Printf("\n  Report  %s\n  JSON    %s\n\n", filepath.Join(dir, "report.html"), filepath.Join(dir, "run.json"))
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(cli(ctx, os.Args[1:]))
}
