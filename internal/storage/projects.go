// Package storage discovers and manages local Blindspot run files.
package storage

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func stateDir() (string, error) {
	if dir := os.Getenv("BLINDSPOT_STATE_DIR"); dir != "" {
		return filepath.Abs(dir)
	}
	dir, err := os.UserConfigDir()
	return filepath.Join(dir, "blindspot"), err
}

// Remember stores only a project path, locally. Separate records avoid lost
// updates when several projects run at once.
func Remember(project string) error {
	dir, err := stateDir()
	if err != nil {
		return err
	}
	return WriteJSON(filepath.Join(dir, "projects", fmt.Sprintf("%x.json", sha256.Sum256([]byte(project)))), project)
}

// WriteJSON atomically replaces a private local metadata file.
func WriteJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".write-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// Expand resolves home-relative and symlinked roots supplied by the user.
func Expand(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(path)
}

// Discover combines registered projects with a directory scan. Explicit roots
// restrict the scan; the default searches the home directory and current path.
// Dependency, hidden, and system trees are skipped, and symlinks are not followed.
func Discover(ctx context.Context, roots []string) ([]string, []string, error) {
	projects := map[string]bool{}
	var warnings []string
	add := func(project string) {
		project, err := Expand(project)
		if err == nil && checkStore(project, false) == nil {
			projects[project] = true
		}
	}
	if len(roots) == 0 {
		dir, err := stateDir()
		if err != nil {
			return nil, nil, err
		}
		entries, err := os.ReadDir(filepath.Join(dir, "projects"))
		if err != nil && !os.IsNotExist(err) {
			warnings = append(warnings, "Could not read the project index: "+err.Error())
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, "projects", entry.Name()))
			var project string
			if err == nil && json.Unmarshal(data, &project) == nil {
				add(project)
			}
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, nil, err
		}
		roots = []string{home}
		if cwd, err := os.Getwd(); err == nil && cwd != home && !strings.HasPrefix(cwd, home+string(os.PathSeparator)) {
			roots = append(roots, cwd)
		}
	}
	seen := map[string]bool{}
	for _, input := range roots {
		root, err := Expand(input)
		if err != nil {
			warnings = append(warnings, err.Error())
			continue
		}
		if seen[root] {
			continue
		}
		seen[root] = true
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				if len(warnings) < 8 {
					warnings = append(warnings, "Could not inspect "+path)
				}
				return filepath.SkipDir
			}
			if !entry.IsDir() {
				return nil
			}
			if entry.Name() == ".blindspot" {
				add(filepath.Dir(path))
				return filepath.SkipDir
			}
			if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules" || entry.Name() == "Library" || entry.Name() == "Applications") {
				return filepath.SkipDir
			}
			return nil
		})
		if err != nil {
			return nil, warnings, err
		}
	}
	result := make([]string, 0, len(projects))
	for project := range projects {
		result = append(result, project)
	}
	sort.Strings(result)
	return result, warnings, nil
}

func checkStore(project string, create bool) error {
	path := filepath.Join(project, ".blindspot")
	if create {
		if err := os.Mkdir(path, 0700); err != nil && !os.IsExist(err) {
			return err
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("refusing a non-directory or symlinked run store: %s", path)
	}
	if info, err := os.Lstat(filepath.Join(path, "runs")); err == nil && !info.IsDir() {
		return fmt.Errorf("refusing a symlinked or non-directory runs folder: %s", filepath.Join(path, "runs"))
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
