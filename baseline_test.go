package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildTimeoutDefaultAndOverride(t *testing.T) {
	for _, c := range []struct {
		opts RunOptions
		want int
	}{{RunOptions{Timeout: 120}, 600}, {RunOptions{Timeout: 900}, 900}, {RunOptions{Timeout: 120, BuildTimeout: 30}, 30}} {
		if got := c.opts.buildTimeoutSeconds(); got != c.want {
			t.Fatalf("%+v: got %d, want %d", c.opts, got, c.want)
		}
	}
}

func TestBaselineSeparatesCompilationAndTestTimeouts(t *testing.T) {
	for _, c := range []struct {
		name, build, test   string
		opts                RunOptions
		status, phase, hint string
		wantTests           bool
	}{
		{"cold build exceeds test budget", "sleep 2", ":", RunOptions{Timeout: 1, BuildTimeout: 4}, "passed", "test", "", true},
		{"build timeout stops before tests", "sleep 2", ":", RunOptions{Timeout: 4, BuildTimeout: 1}, "timeout", "build", "--build-timeout 2", false},
		{"test timeout identified separately", ":", "sleep 2", RunOptions{Timeout: 1, BuildTimeout: 4}, "timeout", "test", "--timeout 2", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			marker := filepath.Join(root, "tests-started")
			t.Setenv("BLINDSPOT_TEST_MARKER", marker)
			script := "#!/bin/sh\ncase \"$1\" in\n build) " + c.build + ";;\n test) touch \"$BLINDSPOT_TEST_MARKER\"; " + c.test + "; printf '%s' '{\"suite\":{\"test_results\":{\"test_ok()\":{\"status\":\"Success\"}}}}';;\n esac\n"
			if err := os.WriteFile(filepath.Join(root, "forge"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
			b, err := runBaseline(context.Background(), root, root, "unused", FoundryConfig{}, c.opts, filepath.Join(root, "logs"))
			if b.Status != c.status || b.Phase != c.phase || (err == nil) != (c.status == "passed") {
				t.Fatalf("unexpected baseline: %+v, %v", b, err)
			}
			if c.hint != "" && !strings.Contains(b.Reason, c.hint) {
				t.Fatalf("missing retry hint: %+v", b)
			}
			if exists(marker) != c.wantTests {
				t.Fatalf("tests started: %v, want %v", exists(marker), c.wantTests)
			}
			if b.DurationMS != b.BuildDurationMS+b.TestDurationMS {
				t.Fatal("phase durations do not add up")
			}
		})
	}
}
