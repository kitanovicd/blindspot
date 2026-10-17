package mutation

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Prepare every copy before starting workers. A copy failure must not leave
// earlier goroutines blocked on a queue that will never receive work.
func prepareWorkers(snapshot, runDir string, count int) ([]string, error) {
	workers := make([]string, 0, count)
	for n := 0; n < count; n++ {
		worker := filepath.Join(runDir, "workers", fmt.Sprint(n))
		if err := copyTree(snapshot, worker, nil); err != nil {
			_ = os.RemoveAll(filepath.Join(runDir, "workers"))
			return nil, fmt.Errorf("prepare worker %d: %w", n, err)
		}
		workers = append(workers, worker)
	}
	return workers, nil
}

// Materialize symlinks so no worker can follow a link back into the user's files.
func copyTree(source, dest string, excludes []string) error {
	root, err := filepath.EvalSymlinks(source)
	if err != nil {
		return err
	}
	omit := map[string]bool{}
	for _, e := range excludes {
		if e == "" {
			continue
		}
		if !filepath.IsAbs(e) {
			e = filepath.Join(root, e)
		}
		omit[filepath.Clean(e)] = true
	}
	var copy func(string, string, map[string]bool) error
	copy = func(from, to string, ancestors map[string]bool) error {
		if omit[from] {
			return nil
		}
		st, err := os.Lstat(from)
		if err != nil {
			return err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			real, err := filepath.EvalSymlinks(from)
			if err != nil {
				return err
			}
			if !inside(root, real) {
				return fmt.Errorf("external symlink is not supported: %s", from)
			}
			if ancestors[real] {
				return fmt.Errorf("circular symlink: %s", from)
			}
			return copy(real, to, ancestors)
		}
		if st.IsDir() {
			if err = os.MkdirAll(to, 0700); err != nil {
				return err
			}
			entries, err := os.ReadDir(from)
			if err != nil {
				return err
			}
			next := map[string]bool{}
			for k, v := range ancestors {
				next[k] = v
			}
			next[from] = true
			for _, e := range entries {
				if e.Name() == ".git" || e.Name() == ".blindspot" || e.Name() == ".DS_Store" {
					continue
				}
				// Saved run stores can outlive a tool or directory rename. Never
				// copy their snapshots into another snapshot, regardless of name.
				if e.IsDir() && strings.HasPrefix(e.Name(), ".") && isRunStore(filepath.Join(from, e.Name())) {
					continue
				}
				if err = copy(filepath.Join(from, e.Name()), filepath.Join(to, e.Name()), next); err != nil {
					return err
				}
			}
			return nil
		}
		if !st.Mode().IsRegular() {
			return fmt.Errorf("unsupported special file: %s", from)
		}
		if err = os.MkdirAll(filepath.Dir(to), 0700); err != nil {
			return err
		}
		in, err := os.Open(from)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, st.Mode().Perm())
		if err != nil {
			return err
		}
		_, err = io.Copy(out, in)
		closeErr := out.Close()
		if err != nil {
			return err
		}
		return closeErr
	}
	return copy(root, dest, map[string]bool{})
}

func isRunStore(dir string) bool {
	f, err := os.Open(filepath.Join(dir, "latest.json"))
	if err != nil {
		return false
	}
	defer f.Close()
	var latest struct {
		ID string `json:"id"`
	}
	if json.NewDecoder(io.LimitReader(f, 4096)).Decode(&latest) != nil || latest.ID == "" || latest.ID == "." || latest.ID == ".." || filepath.Base(latest.ID) != latest.ID {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, "runs", latest.ID, "run.json"))
	return err == nil && info.Mode().IsRegular()
}
