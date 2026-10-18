package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kitanovicd/blindspot/internal/mutation"
	"github.com/kitanovicd/blindspot/internal/storage"
	"github.com/kitanovicd/blindspot/internal/terminal"
	"github.com/kitanovicd/blindspot/internal/toolchain"
)

type projectSettings struct {
	Install       string `json:"install_command"`
	Build         string `json:"build_command"`
	Test          string `json:"test_command"`
	KeepArtifacts *bool  `json:"keep_artifacts,omitempty"`
}

func (a *app) guide(ctx context.Context, opts *mutation.Options, open *bool, flags *flag.FlagSet) error {
	ask := func(label, fallback string) (string, error) { return a.ask(ctx, label, fallback) }
	fmt.Fprintf(a.out, "\n  %s\n  Let's set up your Foundry project. Press Enter to use a default.\n", a.outStyle.Text(terminal.Heading, "BLINDSPOT"))
	for {
		path, err := ask("Where is the project?", opts.Project)
		if err != nil {
			return err
		}
		path = strings.Trim(path, "\"'")
		root, err := mutation.ProjectRoot(path)
		if err != nil {
			fmt.Fprintln(a.out, "  "+err.Error())
			continue
		}
		opts.Project = root
		break
	}
	settings := projectSettings{Build: "forge build", Test: "forge test"}
	for dir := opts.Project; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, ".gitmodules")); err == nil {
			settings.Install = "git submodule update --init --recursive"
			break
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	store, err := storage.ProjectDir(opts.Project)
	if err != nil {
		return err
	}
	file := filepath.Join(store, "settings.json")
	if data, err := os.ReadFile(file); err == nil {
		var saved projectSettings
		if json.Unmarshal(data, &saved) == nil {
			settings = saved
		}
	}
	provided := map[string]bool{}
	flags.Visit(func(f *flag.Flag) { provided[f.Name] = true })
	if provided["install-command"] {
		settings.Install = opts.InstallCommand
	}
	if provided["build-command"] {
		settings.Build = opts.BuildCommand
	}
	if provided["test-command"] {
		settings.Test = opts.TestCommand
	}
	if settings.Build == "" {
		settings.Build = "forge build"
	}
	if settings.Test == "" {
		settings.Test = "forge test"
	}
	if settings.Install == "" {
		settings.Install = "skip"
	}
	settings.Install, err = ask("How do you install dependencies? (or type skip)", settings.Install)
	if err != nil {
		return err
	}
	if strings.EqualFold(settings.Install, "skip") {
		settings.Install = ""
	}
	settings.Build, err = ask("How do you compile the project?", settings.Build)
	if err != nil {
		return err
	}
	settings.Test, err = ask("How do you run the tests?", settings.Test)
	if err != nil {
		return err
	}
	opts.InstallCommand, opts.BuildCommand, opts.TestCommand = settings.Install, settings.Build, settings.Test
	if len(opts.Targets) == 0 && opts.Changed == "" {
		target, err := ask("Which file or directory should I check?", "all")
		if err != nil {
			return err
		}
		if target != "all" {
			opts.Targets = []string{target}
		}
	}
	if !provided["limit"] && !provided["all"] {
		for {
			value, err := ask("Maximum mutations? (0 for all)", strconv.Itoa(opts.Limit))
			if err != nil {
				return err
			}
			limit, err := strconv.Atoi(value)
			if err == nil && limit >= 0 {
				opts.Limit = limit
				break
			}
			fmt.Fprintln(a.out, "  Enter a number greater than or equal to zero.")
		}
	}
	if !provided["open"] {
		for {
			value, err := ask("Open the report when finished? (yes/no)", "yes")
			if err != nil {
				return err
			}
			value = strings.ToLower(value)
			if value == "yes" || value == "y" || value == "no" || value == "n" {
				*open = value == "yes" || value == "y"
				break
			}
			fmt.Fprintln(a.out, "  Enter yes or no.")
		}
	}
	if !provided["keep-artifacts"] {
		keep := false
		if settings.KeepArtifacts != nil {
			keep = *settings.KeepArtifacts
		}
		opts.KeepArtifacts, err = a.confirm(ctx, "Keep project copies and mutation files for replay? Reports are always kept.", keep)
		if err != nil {
			return err
		}
	}
	settings.KeepArtifacts = &opts.KeepArtifacts
	if err := opts.Validate(); err != nil {
		return err
	}
	if err := storage.Remember(opts.Project); err != nil {
		return err
	}
	if err := storage.WriteJSON(file, settings); err != nil {
		return err
	}
	fmt.Fprintln(a.out, "\n  Settings saved in your Blindspot folder.")
	fmt.Fprintln(a.out, "  Setup runs in your project. Mutations run in separate copies.")
	if opts.Gambit == "" {
		if _, err := toolchain.Locate("", "gambit"); err != nil {
			if err := toolchain.InstallGambit(ctx, a.out, a.errOut); err != nil {
				return err
			}
		}
	}
	return nil
}
