package mutation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func exists(file string) bool { _, err := os.Stat(file); return err == nil }

func inside(root, file string) bool {
	rel, err := filepath.Rel(root, file)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}

func findProject(input string) (string, error) {
	p, err := filepath.Abs(input)
	if err != nil {
		return "", err
	}
	p, err = filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	for {
		if exists(filepath.Join(p, "foundry.toml")) {
			return p, nil
		}
		next := filepath.Dir(p)
		if next == p {
			break
		}
		p = next
	}
	return "", fmt.Errorf("no foundry.toml found. Run from a Foundry project or use --project PATH")
}

func sourceFiles(root, src string) ([]string, error) {
	if !filepath.IsAbs(src) {
		src = filepath.Join(root, src)
	}
	var files []string
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".sol") && !strings.HasSuffix(p, ".t.sol") && !strings.HasSuffix(p, ".s.sol") {
			rel, _ := filepath.Rel(root, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}
