package mutation

import "fmt"

// Options defines mutation scope and execution limits. Timeouts are in seconds.
type Options struct {
	Project, Gambit, Solc, Profile string
	Targets                        []string
	Limit, Jobs, Timeout, Line     int
	BuildTimeout                   int
	Seed                           uint64
	AllowFFI                       bool
	Changed                        string
	InstallCommand                 string
	BuildCommand                   string
	TestCommand                    string
	replayLogs                     string
}

// Validate checks scope and worker limits before any project files are created.
func (o Options) Validate() error {
	if o.Jobs < 1 || o.Jobs > 64 || o.Timeout < 1 || o.BuildTimeout < 0 || o.Limit < 0 || o.Line < 0 {
		return fmt.Errorf("jobs must be 1–64, timeout positive, build-timeout, limit and line nonnegative")
	}
	if o.Line > 0 && len(o.Targets) != 1 {
		return fmt.Errorf("--line requires exactly one source file target")
	}
	return nil
}

func (o Options) buildTimeoutSeconds() int {
	if o.BuildTimeout > 0 {
		return o.BuildTimeout
	}
	return max(600, o.Timeout)
}
