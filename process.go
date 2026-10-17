package main

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
)

type ProcessResult struct {
	Code               int
	Stdout, Stderr     string
	StdoutFile         string
	Timeout, Cancelled bool
	DurationMS         int64
	Err                error
}

func tailFile(file string) string {
	f, err := os.Open(file)
	if err != nil {
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	offset := max(int64(0), info.Size()-8*1024*1024)
	_, _ = f.Seek(offset, 0)
	buf := make([]byte, info.Size()-offset)
	n, _ := io.ReadFull(f, buf)
	return string(buf[:n])
}

func execute(parent context.Context, program string, args []string, cwd, logs string, timeout time.Duration, profile string, extraEnv map[string]string) ProcessResult {
	start := time.Now()
	result := ProcessResult{Code: -1}
	if err := os.MkdirAll(logs, 0700); err != nil {
		result.Err = err
		return result
	}
	out, err := os.Create(filepath.Join(logs, "stdout.log"))
	if err != nil {
		result.Err = err
		return result
	}
	defer out.Close()
	result.StdoutFile = out.Name()
	errOut, err := os.Create(filepath.Join(logs, "stderr.log"))
	if err != nil {
		result.Err = err
		return result
	}
	defer errOut.Close()
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = cwd, out, errOut
	env := map[string]string{}
	for _, entry := range os.Environ() {
		k, v, _ := strings.Cut(entry, "=")
		env[k] = v
	}
	// Output paths must stay in the isolated copy even if the caller overrides them.
	for _, k := range []string{"FOUNDRY_OUT", "DAPP_OUT", "FOUNDRY_CACHE_PATH", "DAPP_CACHE_PATH", "FOUNDRY_ROOT", "FOUNDRY_CONFIG"} {
		delete(env, k)
	}
	env["FOUNDRY_PROFILE"], env["NO_COLOR"] = profile, "1"
	for k, v := range extraEnv {
		env[k] = v
	}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	configureProcess(cmd)
	cmd.WaitDelay = 2 * time.Second
	err = cmd.Run()
	if cmd.ProcessState != nil {
		result.Code = cmd.ProcessState.ExitCode()
	}
	result.Cancelled = parent.Err() != nil
	result.Timeout = ctx.Err() == context.DeadlineExceeded && !result.Cancelled
	if err != nil && result.Code == -1 && !result.Timeout && !result.Cancelled {
		result.Err = err
	}
	result.Stdout, result.Stderr = tailFile(out.Name()), tailFile(errOut.Name())
	result.DurationMS = time.Since(start).Milliseconds()
	return result
}

func parseJSON(text string, into any) error {
	if err := json.Unmarshal([]byte(text), into); err == nil {
		return nil
	}
	for i, c := range text {
		if c == '{' && json.Unmarshal([]byte(text[i:]), into) == nil {
			return nil
		}
	}
	return fmt.Errorf("no valid JSON object returned")
}

func classify(result ProcessResult) (string, string, []TestResult) {
	if result.Cancelled {
		return "not-run", "Run interrupted", nil
	}
	if result.Timeout {
		return "timeout", "Test run exceeded its time limit", nil
	}
	if result.Err != nil {
		return "error", result.Err.Error(), nil
	}
	tests, err := processTests(result)
	if err != nil {
		return "error", fmt.Sprintf("Could not read Forge JSON results (exit %d): %v", result.Code, err), nil
	}
	passed, complete := 0, true
	for _, t := range tests {
		if t.Status == "Failure" && result.Code >= 0 {
			reason := t.Reason
			if reason == "" {
				reason = t.Name
			}
			return "caught", reason, tests
		}
		if t.Status == "Success" {
			passed++
		}
		if t.Status != "Success" && t.Status != "Skipped" {
			complete = false
		}
	}
	if result.Code == 0 && passed > 0 && complete {
		return "survived", fmt.Sprintf("%d tests passed", passed), tests
	}
	if len(tests) == 0 {
		return "error", "Forge reported zero test results; check the configured test directory and filters", nil
	}
	return "error", "Forge did not complete successfully", tests
}
