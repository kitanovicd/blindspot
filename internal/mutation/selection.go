package mutation

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

func selectFiles(ctx context.Context, root string, files []string, opts Options) ([]string, error) {
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
