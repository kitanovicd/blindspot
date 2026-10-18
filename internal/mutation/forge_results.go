package mutation

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/kitanovicd/blindspot/internal/report"
)

// The stdout tail is only a diagnostic excerpt. Test evidence must come from
// the complete file, since verbose Forge output can contain large trace arrays.
func processTests(result processResult) ([]report.TestResult, error) {
	if result.ResultsDir != "" {
		return readCommandResults(result.ResultsDir)
	}
	if result.StdoutFile == "" {
		return readForgeResults(strings.NewReader(result.Stdout))
	}
	f, err := os.Open(result.StdoutFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readForgeResults(f)
}

func readForgeResults(input io.Reader) ([]report.TestResult, error) {
	reader := bufio.NewReader(input)
	// Some Forge versions print compilation notices before the JSON document.
	for {
		b, err := reader.Peek(1)
		if err != nil {
			return nil, fmt.Errorf("no JSON results document: %w", err)
		}
		if strings.ContainsRune(" \t\r\n", rune(b[0])) {
			_, _ = reader.ReadByte()
			continue
		}
		if b[0] == '{' {
			break
		}
		for {
			_, err = reader.ReadSlice('\n')
			if err == bufio.ErrBufferFull {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("no JSON results document: %w", err)
			}
			break
		}
	}
	decoder := json.NewDecoder(reader)
	if _, err := decoder.Token(); err != nil { // opening {
		return nil, err
	}
	var tests []report.TestResult
	// Decode one suite at a time, retaining status/reason but not logs, bytecode,
	// traces or other payloads. Memory is bounded by the largest suite document.
	for decoder.More() {
		name, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		suiteName, ok := name.(string)
		if !ok {
			return nil, fmt.Errorf("expected a Forge suite name")
		}
		var suite struct {
			TestResults map[string]struct {
				Status string `json:"status"`
				Reason string `json:"reason"`
			} `json:"test_results"`
		}
		if err := decoder.Decode(&suite); err != nil {
			return nil, fmt.Errorf("suite %q: %w", suiteName, err)
		}
		for testName, t := range suite.TestResults {
			tests = append(tests, report.TestResult{Suite: suiteName, Name: testName, Status: t.Status, Reason: t.Reason})
		}
	}
	if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
		return nil, fmt.Errorf("incomplete Forge results document")
	}
	// Never classify a partial or corrupt document using only the valid prefix.
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("unexpected data after Forge results document")
	}
	sort.Slice(tests, func(i, j int) bool {
		if tests[i].Suite != tests[j].Suite {
			return tests[i].Suite < tests[j].Suite
		}
		return tests[i].Name < tests[j].Name
	})
	return tests, nil
}

func parseJSON(text string, into any) error {
	if err := json.Unmarshal([]byte(text), into); err == nil {
		return nil
	}
	for i, c := range text {
		if c == '{' && json.Unmarshal([]byte(text[i:]), into) == nil {
			return nil
		}
	}
	return fmt.Errorf("no valid JSON object returned")
}

func classify(result processResult) (string, string, []report.TestResult) {
	if result.Cancelled {
		return "not-run", "Run interrupted", nil
	}
	if result.Timeout {
		return "timeout", "Test run exceeded its time limit", nil
	}
	if result.Err != nil {
		return "error", result.Err.Error(), nil
	}
	tests, err := processTests(result)
	if err != nil {
		return "error", fmt.Sprintf("Could not read Forge JSON results (exit %d): %v", result.Code, err), nil
	}
	passed, complete := 0, true
	for _, t := range tests {
		if t.Status == "Failure" && result.Code >= 0 {
			reason := t.Reason
			if reason == "" {
				reason = t.Name
			}
			return "caught", reason, tests
		}
		if t.Status == "Success" {
			passed++
		}
		if t.Status != "Success" && t.Status != "Skipped" {
			complete = false
		}
	}
	if result.Code == 0 && passed > 0 && complete {
		return "survived", fmt.Sprintf("%d tests passed", passed), tests
	}
	if len(tests) == 0 {
		return "error", "Forge reported zero test results; check the configured test directory and filters", nil
	}
	return "error", "Forge did not complete successfully", tests
}
