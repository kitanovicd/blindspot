package report

import (
	_ "embed"
	"encoding/json"
	"os"
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
	return os.WriteFile(file, []byte(html), 0600)
}
