package cli

import (
	"context"
	"fmt"

	"github.com/kitanovicd/blindspot/internal/mutation"
)

func (a *app) doctor(ctx context.Context, args []string) int {
	f := a.flags("doctor")
	project := f.String("project", ".", "Foundry project")
	solc := f.String("solc", "", "Compiler path")
	gambit := f.String("gambit", "", "Gambit path")
	profile := f.String("profile", defaultProfile(), "Foundry profile")
	if err := parseFlags(f, args); err != nil {
		return flagExit(err)
	}
	if f.NArg() != 0 {
		return a.fail(fmt.Errorf("doctor does not accept positional arguments"))
	}
	env, err := mutation.Inspect(ctx, mutation.Options{Project: *project, Solc: *solc, Gambit: *gambit, Profile: *profile})
	if err != nil {
		return a.fail(err)
	}
	fmt.Fprintln(a.out, "\n  Foundry configuration: OK\n  Project:", env.Project, "\n  Profile:", env.Profile)
	if env.GambitError != nil {
		fmt.Fprintln(a.out, "  Gambit:", env.GambitError)
	} else {
		fmt.Fprintln(a.out, "  Gambit:", env.Gambit)
	}
	if env.SolcError != nil {
		fmt.Fprintln(a.out, "  Solidity:", env.SolcError)
	} else {
		fmt.Fprintln(a.out, "  Solidity:", env.Solc)
	}
	if env.FFI {
		fmt.Fprintln(a.out, "  FFI: enabled; add --allow-ffi to your run command (subprocesses are not sandboxed)")
	} else {
		fmt.Fprintln(a.out, "  FFI: disabled")
	}
	fmt.Fprintln(a.out, "  Isolation: project snapshots\n  Runtime: Go binary + Foundry + Gambit + native solc")
	if env.GambitError != nil || env.SolcError != nil {
		return 1
	}
	return 0
}
