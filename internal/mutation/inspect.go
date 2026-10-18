package mutation

import (
	"context"
	"os"
	"path/filepath"

	"github.com/kitanovicd/blindspot/internal/storage"
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
	if err := storage.Remember(root); err != nil {
		return Environment{}, err
	}
	store, err := storage.ProjectDir(root)
	if err != nil {
		return Environment{}, err
	}
	dir, err := os.MkdirTemp(store, ".doctor-*")
	if err != nil {
		return Environment{}, err
	}
	defer os.RemoveAll(dir)
	cfg, err := loadFoundry(ctx, root, filepath.Join(dir, "config"), opts.Profile)
	if err != nil {
		return Environment{}, err
	}
	env := Environment{Project: root, Profile: opts.Profile, FFI: cfg.FFI}
	env.Gambit, env.GambitError = toolchain.Locate(opts.Gambit, "gambit")
	env.Solc, env.SolcError = toolchain.ResolveSolc(opts.Solc, cfg.Solc, root)
	return env, nil
}
