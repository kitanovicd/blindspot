package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/kitanovicd/blindspot/internal/report"
)

var runID = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[0-9a-f]{6}$`)
var artifactNames = []string{"snapshot", "generated", "workers", "replays", "logs", "replay-logs", "compiler.json"}

// Entry describes one run at its actual location, never a path from run.json.
type Entry struct {
	Project, ID, Dir, State string
	Started                 time.Time
	Bytes, ArtifactBytes    int64
	HasReport, Active       bool
	Survived                int
}

func RunDir(project, id string) (string, error) {
	if !runID.MatchString(id) {
		return "", fmt.Errorf("invalid run ID: %s", id)
	}
	if err := checkStore(project, false); err != nil {
		return "", err
	}
	store, err := ProjectDir(project)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(store, "runs")
	for _, path := range []string{dir, filepath.Join(dir, id)} {
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		if !info.IsDir() {
			return "", fmt.Errorf("refusing a symlinked or non-directory run path: %s", path)
		}
	}
	return filepath.Join(dir, id), nil
}

func ReadRun(dir string) (*report.Run, error) {
	path := filepath.Join(dir, "run.json")
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("run.json must be a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var run report.Run
	if err := json.Unmarshal(data, &run); err != nil {
		return nil, err
	}
	if run.SchemaVersion != 1 || run.ID != filepath.Base(dir) {
		return nil, fmt.Errorf("unrecognized run metadata in %s", dir)
	}
	return &run, nil
}

func size(ctx context.Context, path string) (int64, error) {
	var bytes int64
	err := filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			bytes += info.Size()
		}
		return nil
	})
	return bytes, err
}

// List returns recognized saved run directories, including failed setup attempts.
func List(ctx context.Context, projects []string, measure bool) ([]Entry, []string, error) {
	var result []Entry
	var warnings []string
	for _, project := range projects {
		if ctx.Err() != nil {
			return nil, warnings, ctx.Err()
		}
		if err := checkStore(project, false); err != nil {
			warnings = append(warnings, err.Error())
			continue
		}
		store, err := ProjectDir(project)
		if err != nil {
			return nil, warnings, err
		}
		var lockErr error
		if _, err := os.Lstat(filepath.Join(store, ".lock")); !os.IsNotExist(err) {
			var unlock func()
			unlock, lockErr = LockProject(project, true)
			if unlock != nil {
				unlock()
			}
		}
		dirs, err := os.ReadDir(filepath.Join(store, "runs"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			warnings = append(warnings, err.Error())
			continue
		}
		for _, item := range dirs {
			if !item.IsDir() || !runID.MatchString(item.Name()) {
				continue
			}
			dir, err := RunDir(project, item.Name())
			if err != nil {
				warnings = append(warnings, err.Error())
				continue
			}
			started, _ := time.Parse("20060102-150405", item.Name()[:15])
			entry := Entry{Project: project, ID: item.Name(), Dir: dir, Started: started, State: "no report", Active: lockErr != nil}
			if run, err := ReadRun(dir); err == nil {
				entry.State, entry.Survived = run.State, run.Summary.Survived
				// Older binaries do not hold locks. Never delete a legacy run
				// still marked running, whose process cannot be verified.
				if run.State == "running" && !run.ManagedLifecycle {
					entry.Active = true
				}
			}
			info, err := os.Lstat(filepath.Join(dir, "report.html"))
			entry.HasReport = err == nil && info.Mode().IsRegular()
			if entry.HasReport && entry.State == "no report" {
				entry.State = "saved report"
			}
			if measure {
				entry.Bytes, err = size(ctx, dir)
				if err != nil {
					warnings = append(warnings, "Could not measure "+dir+": "+err.Error())
					continue
				}
				for _, name := range artifactNames {
					n, err := size(ctx, filepath.Join(dir, name))
					if err != nil {
						return nil, warnings, err
					}
					entry.ArtifactBytes += n
				}
			}
			result = append(result, entry)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Started.Equal(result[j].Started) {
			return result[i].Dir < result[j].Dir
		}
		return result[i].Started.After(result[j].Started)
	})
	return result, warnings, nil
}

// Prune removes only known artifacts. The caller must hold the project lock.
// Persist the report first; cleanup must never destroy its only copy of results.
func Prune(dir string, run *report.Run, keepLogs bool) error {
	run.ArtifactsRemoved = true
	if err := WriteJSON(filepath.Join(dir, "run.json"), run); err != nil {
		return err
	}
	if err := report.Write(filepath.Join(dir, "report.html"), run); err != nil {
		return err
	}
	for _, name := range artifactNames {
		if keepLogs && (name == "logs" || name == "replay-logs") {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("remove %s: %w", name, err)
		}
	}
	return nil
}

// Clean rechecks the lock and paths at deletion time, rather than trusting an
// earlier preview. It never follows directory symlinks or deletes project files.
func Clean(ctx context.Context, entry Entry, all bool) (int64, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	unlock, err := LockProject(entry.Project, true)
	if err != nil {
		return 0, err
	}
	defer unlock()
	dir, err := RunDir(entry.Project, entry.ID)
	if err != nil {
		return 0, err
	}
	run, readErr := ReadRun(dir)
	if readErr == nil && run.State == "running" && !run.ManagedLifecycle {
		return 0, fmt.Errorf("legacy run is still marked running: %s", entry.ID)
	}
	before, err := size(ctx, dir)
	if err != nil {
		return 0, err
	}
	if all {
		if err := os.RemoveAll(dir); err != nil {
			return 0, err
		}
		return before, repairLatest(entry.Project)
	}
	if readErr != nil {
		return 0, fmt.Errorf("%s has no readable results; use --all to delete this run", entry.ID)
	}
	if err := Prune(dir, run, false); err != nil {
		return 0, err
	}
	after, err := size(ctx, dir)
	return max(0, before-after), err
}

func repairLatest(project string) error {
	dir, err := ProjectDir(project)
	if err != nil {
		return err
	}
	return repairLatestIn(dir)
}

func repairLatestIn(dir string) error {
	entries, err := os.ReadDir(filepath.Join(dir, "runs"))
	if err != nil {
		return err
	}
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if !entry.IsDir() || !runID.MatchString(entry.Name()) {
			continue
		}
		if _, err := ReadRun(filepath.Join(dir, "runs", entry.Name())); err == nil {
			return WriteJSON(filepath.Join(dir, "latest.json"), map[string]string{"id": entry.Name()})
		}
	}
	if err := os.Remove(filepath.Join(dir, "latest.json")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
