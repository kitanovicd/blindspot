package mutation

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type processResult struct {
	Code               int
	Stdout, Stderr     string
	StdoutFile         string
	ResultsDir         string
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

func execute(parent context.Context, program string, args []string, cwd, logs string, timeout time.Duration, profile string, extraEnv map[string]string) processResult {
	start := time.Now()
	result := processResult{Code: -1}
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

func combined(r processResult) string {
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
