package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClassifyRequiresRealTestEvidence(t *testing.T) {
	cases := []struct {
		name   string
		result ProcessResult
		want   string
	}{
		{"success", ProcessResult{Code: 0, Stdout: `{"suite":{"test_results":{"test_ok()":{"status":"Success"}}}}`}, "survived"},
		{"test failure", ProcessResult{Code: 1, Stdout: `{"suite":{"test_results":{"test_bad()":{"status":"Failure","reason":"assertion"}}}}`}, "caught"},
		{"empty suite", ProcessResult{Code: 0, Stdout: `{}`}, "error"},
		{"all skipped", ProcessResult{Code: 0, Stdout: `{"suite":{"test_results":{"test_skip()":{"status":"Skipped"}}}}`}, "error"},
		{"compilation failure", ProcessResult{Code: 1, Stderr: "Compiler run failed"}, "error"},
		{"crashed with partial tests", ProcessResult{Code: -1, Stdout: `{"suite":{"test_results":{"test_bad()":{"status":"Failure"}}}}`}, "error"},
		{"timeout", ProcessResult{Code: -1, Timeout: true}, "timeout"},
		{"cancelled", ProcessResult{Code: -1, Cancelled: true}, "not-run"},
		{"unknown status", ProcessResult{Code: 0, Stdout: `{"suite":{"test_results":{"test_x()":{"status":"SomethingNew"}}}}`}, "error"},
		{"compiler preamble", ProcessResult{Code: 0, Stdout: "Compiling 2 files...\n" + `{"suite":{"test_results":{"test_ok()":{"status":"Success"}}}}`}, "survived"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _, _ := classify(c.result)
			if got != c.want {
				t.Fatalf("got %s, want %s", got, c.want)
			}
		})
	}
}

func TestProcessTimeoutAndCancellation(t *testing.T) {
	dir := t.TempDir()
	r := execute(context.Background(), "/bin/sh", []string{"-c", "sleep 30 & wait"}, dir, filepath.Join(dir, "logs"), 100*time.Millisecond, "default", nil)
	if !r.Timeout || r.DurationMS > 3000 {
		t.Fatalf("timeout not enforced: %+v", r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r = execute(ctx, "/bin/sh", []string{"-c", "sleep 30"}, dir, filepath.Join(dir, "cancel"), time.Minute, "default", nil)
	if !r.Cancelled {
		t.Fatal("cancellation not recorded")
	}
}

func TestProcessCannotInheritOutputDirectory(t *testing.T) {
	t.Setenv("FOUNDRY_OUT", "/dangerous/original/out")
	dir := t.TempDir()
	r := execute(context.Background(), "/bin/sh", []string{"-c", "printf '%s' \"$FOUNDRY_OUT\""}, dir, filepath.Join(dir, "logs"), time.Second, "default", nil)
	if r.Stdout != "" {
		t.Fatal("unsafe output override inherited")
	}
}

func TestSnapshotSymlinksAndExclusions(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(t.TempDir(), "copy")
	if err := os.MkdirAll(filepath.Join(root, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(root, "src", "A.sol"), []byte("original"), 0600)
	_ = os.Symlink(filepath.Join(root, "src"), filepath.Join(root, "linked"))
	_ = os.Mkdir(filepath.Join(root, ".blindspot"), 0700)
	_ = os.WriteFile(filepath.Join(root, ".blindspot", "private"), []byte("skip"), 0600)
	if err := copyTree(root, dest, []string{""}); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(dest, ".blindspot")) {
		t.Fatal("recursive run data copied")
	}
	if err := os.WriteFile(filepath.Join(dest, "linked", "A.sol"), []byte("mutant"), 0600); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "src", "A.sol"))
	if string(data) != "original" {
		t.Fatal("snapshot wrote through a symlink")
	}
	_ = os.Symlink(t.TempDir(), filepath.Join(root, "external"))
	if err := copyTree(root, filepath.Join(t.TempDir(), "bad"), nil); err == nil || !strings.Contains(err.Error(), "external symlink") {
		t.Fatal("external symlink was not rejected")
	}
}

func TestSnapshotExcludesSavedRunStoresAfterRenaming(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, ".saved-results")
	if err := writeJSON(filepath.Join(store, "latest.json"), map[string]string{"id": "saved-run"}); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(store, "runs", "saved-run", "run.json"), &Run{SchemaVersion: 1, ID: "saved-run"}); err != nil {
		t.Fatal(err)
	}
	// A hidden project directory with unrelated JSON must still be copied.
	if err := writeJSON(filepath.Join(root, ".project-data", "latest.json"), map[string]string{"id": "asset"}); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "snapshot")
	if err := copyTree(root, dest, nil); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(dest, ".saved-results")) {
		t.Fatal("saved run store was nested in a new snapshot")
	}
	if !exists(filepath.Join(dest, ".project-data", "latest.json")) {
		t.Fatal("ordinary project data was excluded")
	}
}

func TestSummaryDoesNotCountInvalidOrIncompleteAsCoverage(t *testing.T) {
	r := Run{Mutations: []Mutation{{File: "A.sol", Line: 1, Status: "caught"}, {File: "A.sol", Line: 1, Status: "invalid"}, {File: "A.sol", Line: 2, Status: "survived"}, {File: "A.sol", Line: 2, Status: "caught"}, {File: "A.sol", Line: 3, Status: "timeout"}, {File: "A.sol", Line: 4, Status: "invalid"}}}
	r.Refresh()
	if r.Summary.GapLines != 1 || r.Summary.CaughtLines != 1 || r.Summary.UnresolvedLines != 1 || r.Summary.Invalid != 2 {
		t.Fatalf("wrong summary: %+v", r.Summary)
	}
}

func TestReportEscapesSourceAndRoundTripsData(t *testing.T) {
	file := filepath.Join(t.TempDir(), "report.html")
	r := Run{Files: map[string]string{"evil.sol": "</script><script>alert('x')</script>"}, Mutations: []Mutation{}, Errors: []string{}}
	if err := writeReport(file, &r); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(file)
	if strings.Contains(string(data), "</script><script>alert") {
		t.Fatal("report allowed source code to escape its data script")
	}
	if !strings.Contains(string(data), `\u003c/script\u003e`) {
		t.Fatal("escaped source missing")
	}
}

func TestInterspersedFlags(t *testing.T) {
	got := normalizeArgs([]string{"src/Vault.sol", "--limit", "5", "--open", "--seed=7"}, map[string]bool{"open": true})
	if strings.Join(got, " ") != "--limit 5 --open --seed=7 src/Vault.sol" {
		t.Fatalf("%v", got)
	}
}

func TestRemappingEscapeRejected(t *testing.T) {
	root := t.TempDir()
	_, err := remappings(FoundryConfig{Remappings: []string{"bad/=../other/"}}, root, filepath.Join(root, "snapshot"))
	if err == nil {
		t.Fatal("remapping outside snapshot accepted")
	}
}

func TestFuzzCorpusResetBetweenMutations(t *testing.T) {
	root := t.TempDir()
	snapshot := filepath.Join(t.TempDir(), "snapshot")
	worker := filepath.Join(t.TempDir(), "worker")
	_ = os.MkdirAll(filepath.Join(root, "cache", "fuzz"), 0700)
	_ = os.WriteFile(filepath.Join(root, "cache", "fuzz", "failures"), []byte("baseline corpus"), 0600)
	cfg := FoundryConfig{Fuzz: CorpusConfig{FailurePersistDir: "cache/fuzz"}}
	if err := seedCorpus(cfg, root, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := resetCorpus(snapshot, worker); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(worker, ".blindspot-state", "fuzz", "failures"), []byte("mutant-only failure"), 0600)
	if err := resetCorpus(snapshot, worker); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(worker, ".blindspot-state", "fuzz", "failures"))
	if string(got) != "baseline corpus" {
		t.Fatal("mutant fuzz state leaked into the next test")
	}
}
