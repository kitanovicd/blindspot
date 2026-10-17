package mutation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kitanovicd/blindspot/internal/report"
)

func writeJSON(file string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		return err
	}
	if err = os.WriteFile(file+".tmp", data, 0600); err != nil {
		return err
	}
	return os.Rename(file+".tmp", file)
}

func saveRun(root, dir string, run *report.Run) error {
	if err := writeJSON(filepath.Join(dir, "run.json"), run); err != nil {
		return err
	}
	if err := report.Write(filepath.Join(dir, "report.html"), run); err != nil {
		return err
	}
	return writeJSON(filepath.Join(root, ".blindspot", "latest.json"), map[string]string{"id": run.ID})
}

// LoadRun reads a saved result without running any tests.
func LoadRun(project, id string) (*report.Run, string, error) {
	root, err := findProject(project)
	if err != nil {
		return nil, "", err
	}
	if id == "" {
		data, err := os.ReadFile(filepath.Join(root, ".blindspot", "latest.json"))
		if err != nil {
			return nil, "", fmt.Errorf("no report yet. Run `blindspot run` first")
		}
		var latest struct {
			ID string `json:"id"`
		}
		if err = json.Unmarshal(data, &latest); err != nil {
			return nil, "", err
		}
		id = latest.ID
	}
	dir := filepath.Join(root, ".blindspot", "runs", id)
	if filepath.Base(id) != id || id == "." || id == ".." {
		return nil, "", fmt.Errorf("invalid run ID")
	}
	data, err := os.ReadFile(filepath.Join(dir, "run.json"))
	if err != nil {
		return nil, "", err
	}
	var run report.Run
	if err = json.Unmarshal(data, &run); err != nil {
		return nil, "", err
	}
	if run.SchemaVersion != 1 {
		return nil, "", fmt.Errorf("unsupported report version: %d", run.SchemaVersion)
	}
	return &run, dir, nil
}
