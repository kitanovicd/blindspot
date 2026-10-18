package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// Import moves completed legacy runs from one explicitly selected project.
// This is the only storage operation that visits a project-local .blindspot.
func Import(ctx context.Context, input string) (int, []string, error) {
	project, err := Expand(input)
	if err != nil {
		return 0, nil, err
	}
	project, err = filepath.EvalSymlinks(project)
	if err != nil {
		return 0, nil, err
	}
	legacy := filepath.Join(project, ".blindspot")
	for _, path := range []string{legacy, filepath.Join(legacy, "runs")} {
		if err := directory(path, false); err != nil {
			return 0, nil, err
		}
	}
	releaseLegacy, err := lockDirectory(legacy, true)
	if err != nil {
		return 0, nil, err
	}
	defer releaseLegacy()
	release, err := LockProject(project, true)
	if err != nil {
		return 0, nil, err
	}
	defer release()
	if err := Remember(project); err != nil {
		return 0, nil, err
	}
	store, err := ProjectDir(project)
	if err != nil {
		return 0, nil, err
	}
	if err := os.MkdirAll(filepath.Join(store, "runs"), 0700); err != nil {
		return 0, nil, err
	}
	entries, err := os.ReadDir(filepath.Join(legacy, "runs"))
	if err != nil {
		return 0, nil, err
	}
	var warnings []string
	moved := 0
	for _, entry := range entries {
		if ctx.Err() != nil {
			return moved, warnings, ctx.Err()
		}
		if !entry.IsDir() || !runID.MatchString(entry.Name()) {
			continue
		}
		source := filepath.Join(legacy, "runs", entry.Name())
		run, err := ReadRun(source)
		if err != nil || run.State == "running" {
			warnings = append(warnings, "Skipped unfinished or unreadable run: "+entry.Name())
			continue
		}
		dest := filepath.Join(store, "runs", entry.Name())
		if _, err := os.Lstat(dest); !os.IsNotExist(err) {
			warnings = append(warnings, "Skipped existing or inaccessible destination: "+entry.Name())
			continue
		}
		if err := moveRun(ctx, source, dest, store); err != nil {
			return moved, warnings, err
		}
		moved++
		if err := errors.Join(repairLatest(project), repairLatestIn(legacy)); err != nil {
			return moved, warnings, err
		}
	}
	// Keep the old latest pointer valid when unfinished runs remain.
	if err := repairLatestIn(legacy); err != nil {
		return moved, warnings, err
	}
	if _, err := os.Lstat(filepath.Join(store, "settings.json")); os.IsNotExist(err) {
		old := filepath.Join(legacy, "settings.json")
		if info, err := os.Lstat(old); err == nil && info.Mode().IsRegular() {
			// Preserve the original settings until the user explicitly removes them.
			if err := copyFile(old, filepath.Join(store, "settings.json"), 0600); err != nil {
				return moved, warnings, err
			}
		}
	}
	return moved, warnings, nil
}

func moveRun(ctx context.Context, source, dest, store string) error {
	err := os.Rename(source, dest)
	if err == nil {
		return nil
	}
	if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	stage, err := os.MkdirTemp(store, ".import-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	err = filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(stage, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("cannot import special file: %s", path)
		}
		return copyFile(path, target, info.Mode().Perm())
	})
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(stage, dest); err != nil {
		return err
	}
	return os.RemoveAll(source)
}

func copyFile(source, target string, mode fs.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	return errors.Join(err, out.Close())
}
