package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLargeForgeOutputKeepsCompleteTestEvidence(t *testing.T) {
	for _, c := range []struct{ status, want string }{{"Success", "survived"}, {"Failure", "caught"}} {
		t.Run(c.status, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "verbose.json")
			data := `{"early":{"test_results":{"test_first()":{"status":"` + c.status + `","reason":"assertion","traces":["` + strings.Repeat("a", 9*1024*1024) + `"]}}},"late":{"test_results":{"test_last()":{"status":"Success"}}}}`
			if err := os.WriteFile(file, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			result := execute(context.Background(), "/bin/cat", []string{file}, dir, filepath.Join(dir, "logs"), 10*time.Second, "default", nil)
			if len(result.Stdout) >= len(data) {
				t.Fatal("fixture did not exceed diagnostic excerpt limit")
			}
			status, reason, tests := classify(result)
			if status != c.want || len(tests) != 2 {
				t.Fatalf("got %s / %d tests: %s", status, len(tests), reason)
			}
			if tests[0].Name != "test_first()" || tests[0].Status != c.status {
				t.Fatalf("early result was lost: %+v", tests)
			}
		})
	}
}

func TestForgeMalformedResultsAreNotEmptyOrSuccessful(t *testing.T) {
	valid := `{"suite":{"test_results":{"test_ok()":{"status":"Success"}}}}`
	for _, data := range []string{valid[:len(valid)-1], valid + "trailing garbage", valid + valid, "{broken}", `{"suite":{"test_results":[]}}`} {
		status, reason, _ := classify(ProcessResult{Code: 0, Stdout: data})
		if status != "error" || !strings.Contains(reason, "Could not read Forge JSON") {
			t.Fatalf("malformed document misclassified as %s: %s", status, reason)
		}
	}
	status, reason, _ := classify(ProcessResult{Code: 0, Stdout: "{}"})
	if status != "error" || !strings.Contains(reason, "zero test results") {
		t.Fatalf("empty JSON: %s %s", status, reason)
	}
	status, reason, _ = classify(ProcessResult{Code: 0, Stdout: valid, StdoutFile: filepath.Join(t.TempDir(), "missing")})
	if status != "error" || !strings.Contains(reason, "Could not read Forge JSON") {
		t.Fatalf("missing full output fell back to excerpt: %s %s", status, reason)
	}
}

func TestForgePreambleDoesNotBecomeResultData(t *testing.T) {
	data := "Compiling {a source file}...\n" + strings.Repeat("notice ", 2000) + "\n  {\"suite\":{\"test_results\":{\"test_ok()\":{\"status\":\"Success\",\"reason\":null}}}}\n"
	status, reason, tests := classify(ProcessResult{Code: 0, Stdout: data})
	if status != "survived" || len(tests) != 1 {
		t.Fatalf("preamble parse: %s %s", status, reason)
	}
}
