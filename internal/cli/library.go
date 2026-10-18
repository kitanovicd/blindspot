package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kitanovicd/blindspot/internal/report"
	"github.com/kitanovicd/blindspot/internal/storage"
	"github.com/kitanovicd/blindspot/internal/terminal"
)

type scanPaths []string

func (p *scanPaths) String() string         { return strings.Join(*p, ", ") }
func (p *scanPaths) Set(value string) error { *p = append(*p, value); return nil }

func (a *app) inventory(ctx context.Context, project string, roots []string, measure bool) ([]storage.Entry, error) {
	var projects, warnings []string
	var err error
	if project != "" {
		root, err := storage.Expand(project)
		if err != nil {
			return nil, err
		}
		projects = []string{root}
	} else {
		fmt.Fprintln(a.errOut, "\n  Looking for saved Blindspot runs…")
		projects, warnings, err = storage.Discover(ctx, roots)
		if err != nil {
			return nil, err
		}
	}
	for _, warning := range warnings {
		fmt.Fprintln(a.errOut, "  "+warning)
	}
	entries, warnings, err := storage.List(ctx, projects, measure)
	for _, warning := range warnings {
		fmt.Fprintln(a.errOut, "  "+warning)
	}
	return entries, err
}

func (a *app) reports(ctx context.Context, args []string) int {
	f := a.flags("reports")
	project := f.String("project", "", "List reports in one project")
	list := f.Bool("list", false, "List reports without opening a menu")
	var roots scanPaths
	f.Var(&roots, "scan", "Search a directory instead of the default locations; repeatable")
	if err := parseFlags(f, args); err != nil {
		return flagExit(err)
	}
	if f.NArg() != 0 || (*project != "" && len(roots) > 0) {
		return a.fail(fmt.Errorf("use --project or --scan, without positional arguments"))
	}
	entries, err := a.inventory(ctx, *project, roots, false)
	if err != nil {
		return a.promptError(err)
	}
	var ready []storage.Entry
	var choices []terminal.Choice
	for _, entry := range entries {
		if !entry.HasReport {
			continue
		}
		ready = append(ready, entry)
		choices = append(choices, terminal.Choice{Title: runTitle(entry), Detail: entry.Project + " · " + entry.ID})
	}
	if len(ready) == 0 {
		return a.text("\n  No saved reports found. Use --scan PATH to search another location.\n")
	}
	if *list || !terminal.IsTerminal(os.Stdin) {
		for _, entry := range ready {
			fmt.Fprintf(a.out, "\n  %s\n  %s\n", runTitle(entry), filepath.Join(entry.Dir, "report.html"))
		}
		return 0
	}
	selected, err := a.choose(ctx, "Saved reports — newest first", choices, false)
	if err != nil {
		return a.promptError(err)
	}
	entry := ready[selected[0]]
	unlock, err := storage.LockProject(entry.Project, false)
	if err != nil {
		return a.fail(err)
	}
	defer unlock()
	dir, err := storage.RunDir(entry.Project, entry.ID)
	if err != nil {
		return a.fail(err)
	}
	file := filepath.Join(dir, "report.html")
	info, err := os.Lstat(file)
	if err != nil {
		return a.fail(err)
	}
	if !info.Mode().IsRegular() {
		return a.fail(fmt.Errorf("report must be a regular file"))
	}
	if ctx.Err() != nil {
		return 130
	}
	if err := report.Open(file); err != nil {
		return a.fail(err)
	}
	fmt.Fprintln(a.out, "\n  Opened "+file)
	return 0
}

func runTitle(entry storage.Entry) string {
	state := entry.State
	if entry.Active {
		state += " · active or unverified"
	}
	return fmt.Sprintf("%s · %s · %s · %d survived", filepath.Base(entry.Project), entry.Started.Local().Format("2006-01-02 15:04"), state, entry.Survived)
}

func bytesLabel(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	value := float64(size)
	for _, unit := range []string{"KiB", "MiB", "GiB", "TiB"} {
		value /= 1024
		if value < 1024 || unit == "TiB" {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
	}
	return ""
}

func (a *app) clean(ctx context.Context, args []string) int {
	f := a.flags("clean")
	project := f.String("project", "", "Clean one project's saved runs")
	all := f.Bool("all", false, "Delete entire saved runs, including reports")
	yes := f.Bool("yes", false, "Delete without prompting after displaying the plan")
	dry := f.Bool("dry-run", false, "Preview files and sizes without deleting")
	older := f.String("older-than", "", "Only runs older than a duration, for example 30d or 24h")
	id := f.String("run", "", "Only this run ID; requires --project")
	var roots scanPaths
	f.Var(&roots, "scan", "Search a directory instead of the default locations; repeatable")
	if err := parseFlags(f, args); err != nil {
		return flagExit(err)
	}
	if f.NArg() != 0 || (*project != "" && len(roots) > 0) || (*id != "" && *project == "") {
		return a.fail(fmt.Errorf("use --project or --scan; --run requires --project"))
	}
	var cutoff time.Time
	if *older != "" {
		value := *older
		factor := time.Duration(1)
		if strings.HasSuffix(value, "d") {
			value = strings.TrimSuffix(value, "d") + "h"
			factor = 24
		}
		age, err := time.ParseDuration(value)
		if err != nil || age <= 0 || age > time.Duration(1<<63-1)/factor {
			return a.fail(fmt.Errorf("--older-than must be a positive duration such as 30d or 24h"))
		}
		cutoff = time.Now().Add(-age * factor)
	}
	entries, err := a.inventory(ctx, *project, roots, true)
	if err != nil {
		return a.promptError(err)
	}
	filtered := entries[:0]
	for _, entry := range entries {
		if (*id == "" || entry.ID == *id) && (cutoff.IsZero() || entry.Started.Before(cutoff)) {
			filtered = append(filtered, entry)
		}
	}
	entries = filtered
	if len(entries) == 0 {
		return a.text("\n  No matching saved runs found.\n")
	}
	fmt.Fprintln(a.out, "\n  Saved Blindspot files")
	for _, entry := range entries {
		fmt.Fprintf(a.out, "\n  %s · %s\n  %s\n", runTitle(entry), bytesLabel(entry.Bytes), entry.Dir)
	}
	interactive := terminal.IsTerminal(os.Stdin) && !*yes && !*dry
	if interactive && !*all {
		selection, err := a.choose(ctx, "What should be removed?", []terminal.Choice{
			{Title: "Replay and temporary files", Detail: "Keep HTML reports and JSON results; replay becomes unavailable"},
			{Title: "Entire saved runs", Detail: "Delete the selected reports and all their artifacts"},
		}, false)
		if err != nil {
			return a.promptError(err)
		}
		*all = selection[0] == 1
	}
	var candidates []storage.Entry
	var choices []terminal.Choice
	for _, entry := range entries {
		if entry.Active {
			fmt.Fprintf(a.out, "\n  Skipping active or unverified run: %s\n", entry.ID)
			continue
		}
		amount := entry.Bytes
		if !*all {
			amount = entry.ArtifactBytes
			if !entry.HasReport {
				fmt.Fprintf(a.out, "\n  %s has no report; use --all to remove the failed setup files.\n", entry.ID)
				continue
			}
		}
		if amount == 0 && !*all {
			continue
		}
		candidates = append(candidates, entry)
		choices = append(choices, terminal.Choice{Title: runTitle(entry) + " · " + bytesLabel(amount), Detail: entry.Dir})
	}
	if len(candidates) == 0 {
		return a.text("\n  Nothing eligible to remove.\n")
	}
	if interactive && *id == "" {
		selected, err := a.choose(ctx, "Select runs to clean", choices, true)
		if err != nil {
			return a.promptError(err)
		}
		var picked []storage.Entry
		for _, i := range selected {
			picked = append(picked, candidates[i])
		}
		candidates = picked
	}
	if len(candidates) == 0 {
		return a.text("\n  No runs selected. Nothing deleted.\n")
	}
	var total int64
	fmt.Fprintln(a.out, "\n  Cleanup preview")
	for _, entry := range candidates {
		amount := entry.ArtifactBytes
		if *all {
			amount = entry.Bytes
		}
		total += amount
		fmt.Fprintf(a.out, "  %s  %s\n", bytesLabel(amount), entry.Dir)
	}
	mode := "replay and temporary files; reports stay"
	if *all {
		mode = "entire runs, including reports"
	}
	fmt.Fprintf(a.out, "\n  %d runs · about %s reclaimable\n  Remove %s.\n", len(candidates), bytesLabel(total), mode)
	if *dry {
		return a.text("  Preview only. Nothing deleted.\n")
	}
	if !*yes {
		if !interactive {
			return a.text("  Nothing deleted. Pass --yes to apply this cleanup.\n")
		}
		approved, err := a.confirm(ctx, "Delete the files shown above?", false)
		if err != nil {
			return a.promptError(err)
		}
		if !approved {
			return a.text("\n  Nothing deleted.\n")
		}
	}
	var freed int64
	failed, cleaned := 0, 0
	for _, entry := range candidates {
		if ctx.Err() != nil {
			return 130
		}
		amount, err := storage.Clean(ctx, entry, *all)
		if err != nil {
			fmt.Fprintf(a.errOut, "  Skipped %s: %v\n", entry.ID, err)
			failed++
			continue
		}
		freed += amount
		cleaned++
		fmt.Fprintf(a.out, "  Cleaned %s · %s removed\n", entry.ID, bytesLabel(amount))
	}
	fmt.Fprintf(a.out, "\n  Cleaned %d runs · %s removed\n", cleaned, bytesLabel(freed))
	if failed > 0 {
		return 1
	}
	return 0
}
