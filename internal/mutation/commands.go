package mutation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kitanovicd/blindspot/internal/report"
)

const forgeContextEnv = "BLINDSPOT_FORGE_CONTEXT"

type forgeContext struct {
	Executable string              `json:"executable"`
	Arguments  map[string][]string `json:"arguments"`
	Results    string              `json:"results"`
}

type forgeExit struct {
	Code  int    `json:"code"`
	Error string `json:"error,omitempty"`
}

func runForge(ctx context.Context, command, worker, root, solc string, cfg foundryConfig, opts Options, logs string) processResult {
	args, err := forgeArgs(command, worker, root, solc, cfg, opts.Seed)
	if err != nil {
		return processResult{Code: -1, Err: err}
	}
	limit := time.Duration(opts.buildTimeoutSeconds()) * time.Second
	script := opts.BuildCommand
	if command == "test" {
		limit = time.Duration(opts.Timeout) * time.Second
		script = opts.TestCommand
	}
	env := workerEnv(cfg, root, worker)
	if strings.TrimSpace(script) == "" {
		return execute(ctx, "forge", args, worker, logs, limit, opts.Profile, env)
	}
	bridge, err := prepareForgeBridge(logs, worker, root, solc, cfg, opts)
	if err != nil {
		return processResult{Code: -1, Err: err}
	}
	env[forgeContextEnv] = bridge
	env["PATH"] = filepath.Join(logs, "bin") + string(os.PathListSeparator) + os.Getenv("PATH")
	result := execute(ctx, "/bin/sh", []string{"-c", script}, worker, logs, limit, opts.Profile, env)
	if command == "test" {
		result.ResultsDir = filepath.Join(logs, "forge-results")
	}
	return result
}

func prepareForgeBridge(logs, worker, root, solc string, cfg foundryConfig, opts Options) (string, error) {
	forge, err := exec.LookPath("forge")
	if err != nil {
		return "", err
	}
	forge, err = filepath.Abs(forge)
	if err != nil {
		return "", err
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	state := forgeContext{Executable: forge, Arguments: map[string][]string{}, Results: filepath.Join(logs, "forge-results")}
	for _, command := range []string{"build", "test"} {
		state.Arguments[command], err = forgeArgs(command, worker, root, solc, cfg, opts.Seed)
		if err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(filepath.Join(logs, "bin"), 0700); err != nil {
		return "", err
	}
	path := filepath.Join(logs, "forge-context.json")
	if err := writeJSON(path, state); err != nil {
		return "", err
	}
	// A PATH shim also covers Forge invoked by Makefiles and shell scripts.
	quoted := "'" + strings.ReplaceAll(executable, "'", "'\\''") + "'"
	script := "#!/bin/sh\nexec " + quoted + " _forge \"$@\"\n"
	if err := os.WriteFile(filepath.Join(logs, "bin", "forge"), []byte(script), 0700); err != nil {
		return "", err
	}
	return path, nil
}

// ForwardForge is the private entry point used by command wrappers. The real
// process stays in its parent's process group so cancellation kills the full tree.
func ForwardForge(ctx context.Context, args []string, input io.Reader, output, errors io.Writer) int {
	fail := func(err error) int { fmt.Fprintln(errors, "blindspot:", err); return 1 }
	path := os.Getenv(forgeContextEnv)
	if path == "" {
		return fail(fmt.Errorf("_forge is reserved for Blindspot command execution"))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fail(err)
	}
	var state forgeContext
	if err := json.Unmarshal(data, &state); err != nil {
		return fail(err)
	}
	command, args := forgeCommand(args)
	if managed, ok := state.Arguments[command]; ok {
		args = managedForgeArgs(args, managed)
	}
	var capture *os.File
	var resultDir string
	if command == "test" {
		if err := os.MkdirAll(state.Results, 0700); err != nil {
			return fail(err)
		}
		resultDir, err = os.MkdirTemp(state.Results, "test-")
		if err != nil {
			return fail(err)
		}
		capture, err = os.Create(filepath.Join(resultDir, "stdout.log"))
		if err != nil {
			return fail(err)
		}
		defer capture.Close()
		output = io.MultiWriter(output, capture)
	}
	cmd := exec.CommandContext(ctx, state.Executable, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = input, output, errors
	cmd.WaitDelay = 2 * time.Second
	err = cmd.Run()
	result := forgeExit{Code: -1}
	if cmd.ProcessState != nil {
		result.Code = cmd.ProcessState.ExitCode()
	}
	if err != nil && result.Code < 0 {
		result.Error = err.Error()
	}
	if capture != nil {
		if err := capture.Close(); err != nil {
			return fail(err)
		}
		if err := writeJSON(filepath.Join(resultDir, "exit.json"), result); err != nil {
			return fail(err)
		}
	}
	if result.Code < 0 {
		return fail(err)
	}
	return result.Code
}

func managedForgeArgs(input, managed []string) []string {
	// Preserve project filters and build options, while keeping the compiler,
	// paths, seed, and structured output consistent in every isolated copy.
	values := map[string]bool{
		"--root": true, "--use": true, "--out": true, "-o": true,
		"--cache-path": true, "--contracts": true, "-C": true,
		"--threads": true, "--jobs": true, "-j": true, "--fuzz-seed": true,
	}
	result := []string{input[0]}
	for i := 1; i < len(input); i++ {
		if input[i] == "--" {
			result = append(result, managed[1:]...)
			return append(result, input[i:]...)
		}
		name, _, inline := strings.Cut(input[i], "=")
		if values[name] {
			if !inline && i+1 < len(input) {
				i++
			}
			continue
		}
		if len(name) > 2 && (strings.HasPrefix(name, "-j") || strings.HasPrefix(name, "-o") || strings.HasPrefix(name, "-C")) {
			continue
		}
		if name == "--json" || name == "--quiet" || name == "-q" || name == "--junit" || name == "--summary" || name == "--detailed" {
			continue
		}
		result = append(result, input[i])
	}
	return append(result, managed[1:]...)
}

// Normalize aliases and leading global options before injecting managed flags.
func forgeCommand(args []string) (string, []string) {
	for i := 0; i < len(args); i++ {
		name, _, inline := strings.Cut(args[i], "=")
		if strings.HasPrefix(name, "-") {
			if !inline && (name == "--color" || name == "--threads" || name == "--jobs" || name == "-j") {
				i++
			}
			continue
		}
		command := args[i]
		switch command {
		case "b", "compile":
			command = "build"
		case "t":
			command = "test"
		}
		if command == "build" || command == "test" {
			normalized := append([]string{command}, args[:i]...)
			return command, append(normalized, args[i+1:]...)
		}
		return command, args
	}
	return "", args
}

func readCommandResults(dir string) ([]report.TestResult, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("test command did not execute Forge tests; invoke forge through PATH inside the project copy")
		}
		return nil, err
	}
	var tests []report.TestResult
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(filepath.Join(path, "exit.json"))
		if err != nil {
			return nil, fmt.Errorf("incomplete Forge invocation: %w", err)
		}
		var exit forgeExit
		if err := json.Unmarshal(data, &exit); err != nil {
			return nil, err
		}
		result := processResult{Code: exit.Code, StdoutFile: filepath.Join(path, "stdout.log")}
		if exit.Error != "" {
			return nil, fmt.Errorf("Forge process failed: %s", exit.Error)
		}
		status, reason, results := classify(result)
		if status != "caught" && status != "survived" {
			return nil, fmt.Errorf("Forge invocation failed: %s", reason)
		}
		tests = append(tests, results...)
	}
	return tests, nil
}
