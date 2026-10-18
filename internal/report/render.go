package report

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

//go:embed template.html
var reportTemplate string

// Write renders a self-contained HTML report, escaping embedded source data.
func Write(file string, run *Run) error {
	data, err := json.Marshal(run)
	if err != nil {
		return err
	}
	// encoding/json escapes HTML delimiters, preventing source code from closing the script tag.
	html := strings.Replace(reportTemplate, "__RUN_DATA__", string(data), 1)
	f, err := os.CreateTemp(filepath.Dir(file), ".report-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(html); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), file)
}
