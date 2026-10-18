// Package storage manages Blindspot's dedicated local data directory.
package storage

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Home is the only root used to store and discover Blindspot data. Computing
// paths must not inspect source projects or trigger access to protected folders.
func Home() (string, error) {
	dir := os.Getenv("BLINDSPOT_HOME")
	if dir == "" {
		dir = os.Getenv("BLINDSPOT_STATE_DIR")
	}
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".blindspot")
	}
	return Expand(dir)
}

// Expand is lexical: saved project paths remain usable without filesystem access.
func Expand(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
	}
	return filepath.Abs(path)
}

func ProjectDir(project string) (string, error) {
	home, err := Home()
	if err != nil {
		return "", err
	}
	project, err = Expand(project)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "projects", fmt.Sprintf("%x", sha256.Sum256([]byte(project)))), nil
}

func Remember(project string) error {
	if err := checkStore(project, true); err != nil {
		return err
	}
	dir, err := ProjectDir(project)
	if err != nil {
		return err
	}
	return WriteJSON(filepath.Join(dir, "project.json"), project)
}

// Projects reads only records inside the dedicated store. Source project paths
// are labels, never directories to visit while browsing or cleaning saved runs.
func Projects(ctx context.Context) ([]string, error) {
	home, err := Home()
	if err != nil {
		return nil, err
	}
	for _, dir := range []string{home, filepath.Join(home, "projects")} {
		if err := directory(dir, false); os.IsNotExist(err) {
			return nil, nil
		} else if err != nil {
			return nil, err
		}
	}
	entries, err := os.ReadDir(filepath.Join(home, "projects"))
	if err != nil {
		return nil, err
	}
	var result []string
	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(home, "projects", entry.Name())
		file := filepath.Join(dir, "project.json")
		info, err := os.Lstat(file)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		data, err := os.ReadFile(file)
		var project string
		if err != nil || json.Unmarshal(data, &project) != nil || !filepath.IsAbs(project) {
			continue
		}
		expected, err := ProjectDir(project)
		if err == nil && expected == dir {
			result = append(result, project)
		}
	}
	sort.Strings(result)
	return result, nil
}

// ResolveProject supports a saved project's subdirectories without searching
// for foundry.toml or requiring the original project to exist.
func ResolveProject(ctx context.Context, input string) (string, error) {
	path, err := Expand(input)
	if err != nil {
		return "", err
	}
	projects, err := Projects(ctx)
	if err != nil {
		return "", err
	}
	best := ""
	for _, project := range projects {
		if (path == project || strings.HasPrefix(path, project+string(os.PathSeparator))) && len(project) > len(best) {
			best = project
		}
	}
	if best != "" {
		return best, nil
	}
	return path, nil
}

func checkStore(project string, create bool) error {
	dir, err := ProjectDir(project)
	if err != nil {
		return err
	}
	for _, path := range []string{filepath.Dir(filepath.Dir(dir)), filepath.Dir(dir), dir} {
		if err := directory(path, create); err != nil {
			return err
		}
	}
	if err := directory(filepath.Join(dir, "runs"), false); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func directory(path string, create bool) error {
	if create {
		if err := os.MkdirAll(path, 0700); err != nil && !os.IsExist(err) {
			return err
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("refusing a symlinked or non-directory Blindspot folder: %s", path)
	}
	return nil
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
