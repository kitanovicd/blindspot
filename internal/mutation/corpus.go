package mutation

import (
	"fmt"
	"os"
	"path/filepath"
)

func seedCorpus(cfg foundryConfig, root, snapshot string) error {
	for name, dir := range map[string]string{"fuzz": cfg.Fuzz.FailurePersistDir, "invariant": cfg.Invariant.FailurePersistDir} {
		if dir == "" {
			continue
		}
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(root, dir)
		}
		if !inside(root, dir) {
			return fmt.Errorf("external failure corpus is not supported: %s", dir)
		}
		if exists(dir) {
			if err := copyTree(dir, filepath.Join(snapshot, ".blindspot-state", name), nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func resetCorpus(snapshot, worker string) error {
	dest := filepath.Join(worker, ".blindspot-state")
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	source := filepath.Join(snapshot, ".blindspot-state")
	if exists(source) {
		return copyTree(source, dest, nil)
	}
	return os.MkdirAll(dest, 0700)
}
