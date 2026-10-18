package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kitanovicd/blindspot/internal/mutation"
	"github.com/kitanovicd/blindspot/internal/report"
	"github.com/kitanovicd/blindspot/internal/storage"
)

func (a *app) report(ctx context.Context, args []string) int {
	f := a.flags("report")
	project := f.String("project", ".", "Foundry project")
	id := f.String("run", "", "Run ID (default: latest)")
	serve := f.Bool("serve", false, "Serve on localhost")
	port := f.Int("port", 0, "Local port (default: automatic)")
	noOpen := f.Bool("no-open", false, "Do not open browser")
	export := f.String("export", "", "Export self-contained HTML")
	if err := parseFlags(f, args); err != nil {
		return flagExit(err)
	}
	if f.NArg() != 0 {
		return a.fail(fmt.Errorf("report does not accept positional arguments"))
	}
	if *port < 0 || *port > 65535 {
		return a.fail(fmt.Errorf("port must be between 0 and 65535"))
	}
	root, err := storage.ResolveProject(ctx, *project)
	if err != nil {
		return a.fail(err)
	}
	unlock, err := storage.LockProject(root, false)
	if err != nil {
		return a.fail(err)
	}
	defer unlock()
	run, dir, err := mutation.LoadRun(root, *id)
	if err != nil {
		return a.fail(err)
	}
	file := filepath.Join(dir, "report.html")
	run.Refresh()
	run.Executable, _ = os.Executable()
	if err = report.Write(file, run); err != nil {
		return a.fail(err)
	}
	if *export != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return a.fail(err)
		}
		if err = os.WriteFile(*export, data, 0600); err != nil {
			return a.fail(err)
		}
		fmt.Fprintln(a.out, "Report exported to", *export)
	}
	if *serve {
		if err = report.Serve(ctx, file, *port, !*noOpen, a.out); err != nil {
			return a.fail(err)
		}
	} else if !*noOpen {
		if err = report.Open(file); err != nil {
			return a.fail(err)
		}
	} else {
		fmt.Fprintln(a.out, file)
	}
	return 0
}
