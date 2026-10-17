package main

import "time"

const Version = "0.1.0"

type Mutation struct {
	ID          string       `json:"id"`
	File        string       `json:"file"`
	Line        int          `json:"line"`
	Column      int          `json:"column"`
	Operator    string       `json:"operator"`
	Original    string       `json:"original"`
	Replacement string       `json:"replacement"`
	Diff        string       `json:"diff"`
	Artifact    string       `json:"artifact"`
	Status      string       `json:"status"`
	Reason      string       `json:"reason"`
	DurationMS  int64        `json:"duration_ms"`
	TestsRun    int          `json:"tests_run"`
	Failures    []TestResult `json:"failures"`
	Log         string       `json:"log"`
}

type TestResult struct {
	Suite  string `json:"suite"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type Baseline struct {
	Status          string `json:"status"`
	Phase           string `json:"phase,omitempty"`
	Reason          string `json:"reason,omitempty"`
	Tests           int    `json:"tests"`
	DurationMS      int64  `json:"duration_ms"`
	BuildDurationMS int64  `json:"build_duration_ms"`
	TestDurationMS  int64  `json:"test_duration_ms"`
	Log             string `json:"log"`
}

type Run struct {
	SchemaVersion       int               `json:"schema_version"`
	ID                  string            `json:"id"`
	Project             string            `json:"project"`
	Executable          string            `json:"executable"`
	StartedAt           time.Time         `json:"started_at"`
	DurationMS          int64             `json:"duration_ms"`
	State               string            `json:"state"`
	Seed                uint64            `json:"seed"`
	Limit               int               `json:"limit"`
	Candidates          int               `json:"candidates"`
	Scope               string            `json:"scope"`
	Profile             string            `json:"profile"`
	FFI                 bool              `json:"ffi"`
	ForgeVersion        string            `json:"forge_version"`
	Gambit              string            `json:"gambit"`
	Solc                string            `json:"solc"`
	TimeoutSeconds      int               `json:"timeout_seconds"`
	BuildTimeoutSeconds int               `json:"build_timeout_seconds"`
	Baseline            Baseline          `json:"baseline"`
	Files               map[string]string `json:"files"`
	Mutations           []Mutation        `json:"mutations"`
	Errors              []string          `json:"errors"`
	Summary             Summary           `json:"summary"`
}

type Summary struct {
	Total           int `json:"total"`
	Caught          int `json:"caught"`
	Survived        int `json:"survived"`
	Invalid         int `json:"invalid"`
	Timeout         int `json:"timeout"`
	Error           int `json:"error"`
	NotRun          int `json:"not_run"`
	GapLines        int `json:"gap_lines"`
	CaughtLines     int `json:"caught_lines"`
	UnresolvedLines int `json:"unresolved_lines"`
}

func (r *Run) Refresh() {
	s := Summary{}
	type loc struct {
		file string
		line int
	}
	lines := map[loc][]string{}
	for _, m := range r.Mutations {
		s.Total++
		switch m.Status {
		case "caught":
			s.Caught++
		case "survived":
			s.Survived++
		case "invalid":
			s.Invalid++
		case "timeout":
			s.Timeout++
		case "error":
			s.Error++
		default:
			s.NotRun++
		}
		k := loc{m.File, m.Line}
		lines[k] = append(lines[k], m.Status)
	}
	for _, states := range lines {
		gap, hasCaught, unresolved := false, false, false
		for _, state := range states {
			gap = gap || state == "survived"
			hasCaught = hasCaught || state == "caught"
			unresolved = unresolved || state == "error" || state == "timeout" || state == "not-run"
		}
		if gap {
			s.GapLines++
		}
		if hasCaught && !gap && !unresolved {
			s.CaughtLines++
		}
		if unresolved {
			s.UnresolvedLines++
		}
	}
	r.Summary = s
}
