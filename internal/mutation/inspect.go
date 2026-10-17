package mutation

import (
	"context"
	"path/filepath"

	"github.com/kitanovicd/blindspot/internal/toolchain"
)

// Environment describes the effective project configuration and available tools.
type Environment struct {
	Project, Profile, Gambit, Solc string
	FFI                            bool
	GambitError, SolcError         error
}

// Inspect reads configuration and resolves tools without executing project tests.
func Inspect(ctx context.Context, opts Options) (Environment, error) {
	root, err := findProject(opts.Project)
	if err != nil {
		return Environment{}, err
	}
	cfg, err := loadFoundry(ctx, root, filepath.Join(root, ".blindspot", "doctor"), opts.Profile)
	if err != nil {
		return Environment{}, err
	}
	env := Environment{Project: root, Profile: opts.Profile, FFI: cfg.FFI}
	env.Gambit, env.GambitError = toolchain.Locate(opts.Gambit, "gambit")
	env.Solc, env.SolcError = toolchain.ResolveSolc(opts.Solc, cfg.Solc, root)
	return env, nil
}
