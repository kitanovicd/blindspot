package mutation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kitanovicd/blindspot/internal/report"
	"github.com/kitanovicd/blindspot/internal/storage"
)

func writeJSON(file string, value any) error {
	return storage.WriteJSON(file, value)
}

func saveRun(root, dir string, run *report.Run) error {
	if err := writeJSON(filepath.Join(dir, "run.json"), run); err != nil {
		return err
	}
	if err := report.Write(filepath.Join(dir, "report.html"), run); err != nil {
		return err
	}
	store, err := storage.ProjectDir(root)
	if err != nil {
		return err
	}
	return writeJSON(filepath.Join(store, "latest.json"), map[string]string{"id": run.ID})
}

// LoadRun reads a saved result without running any tests.
func LoadRun(project, id string) (*report.Run, string, error) {
	store, err := storage.ProjectDir(project)
	if err != nil {
		return nil, "", err
	}
	if id == "" {
		data, err := os.ReadFile(filepath.Join(store, "latest.json"))
		if err != nil {
			return nil, "", fmt.Errorf("no saved report for this project. Run `blindspot run` first")
		}
		var latest struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(data, &latest); err != nil {
			return nil, "", err
		}
		id = latest.ID
	}
	dir, err := storage.RunDir(project, id)
	if err != nil {
		return nil, "", err
	}
	run, err := storage.ReadRun(dir)
	return run, dir, err
}
