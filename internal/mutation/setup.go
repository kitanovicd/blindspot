package mutation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ProjectRoot resolves a Foundry project before its dependencies are installed.
func ProjectRoot(input string) (string, error) {
	if input == "~" || strings.HasPrefix(input, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if input == "~" {
			input = home
		} else {
			input = filepath.Join(home, input[2:])
		}
	}
	return findProject(input)
}

func (r *Runner) prepareProject(ctx context.Context, root, logs string, opts Options) error {
	r.section("Setup")
	limit := time.Duration(opts.buildTimeoutSeconds()) * time.Second
	env := map[string]string{}
	if opts.Solc != "" {
		env["FOUNDRY_SOLC"] = opts.Solc
	}
	build := opts.BuildCommand
	if build == "" {
		build = "forge build"
	}
	for _, step := range []struct{ name, command string }{
		{"dependencies", opts.InstallCommand},
		{"build", build},
	} {
		if strings.TrimSpace(step.command) == "" {
			continue
		}
		label := "Installing dependencies"
		if step.name == "build" {
			label = "Building the project"
		}
		result := r.progressStep(label, limit, func() processResult {
			return execute(ctx, "/bin/sh", []string{"-c", step.command}, root, filepath.Join(logs, step.name), limit, opts.Profile, env)
		})
		if result.Cancelled {
			return fmt.Errorf("project setup interrupted")
		}
		if result.Timeout {
			return fmt.Errorf("%s timed out after %s; increase --build-timeout. See %s", label, limit, filepath.Join(logs, step.name))
		}
		if result.Err != nil || result.Code != 0 {
			return fmt.Errorf("%s failed (exit %d). See %s\n%s", label, result.Code, filepath.Join(logs, step.name), shortLog(combined(result)))
		}
		r.logf("  %s finished in %s", label, time.Duration(result.DurationMS)*time.Millisecond)
	}
	return nil
}
