package toolchain

import (
	"os"
	"path/filepath"
	"regexp"
)

var compilerVersion = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// SVM keeps the legacy directory when present, otherwise it uses the OS data
// directory. Search both so existing installs remain usable after migrations.
// https://github.com/alloy-rs/svm-rs/blob/master/crates/svm-rs/src/paths.rs
func svmDirectories(home, platform, xdgData, appData string) []string {
	dirs := []string{filepath.Join(home, ".svm")}
	switch platform {
	case "darwin":
		dirs = append(dirs, filepath.Join(home, "Library", "Application Support", "svm"))
	case "windows":
		if appData != "" {
			dirs = append(dirs, filepath.Join(appData, "svm"))
		}
	default:
		if !filepath.IsAbs(xdgData) {
			xdgData = filepath.Join(home, ".local", "share")
		}
		dirs = append(dirs, filepath.Join(xdgData, "svm"))
	}
	return dirs
}

func findSVMCompiler(dirs []string, version string) string {
	var matches []string
	seen := map[string]bool{}
	for _, dir := range dirs {
		versions := []string{version}
		if version == "" {
			versions = nil
			entries, _ := os.ReadDir(dir)
			for _, entry := range entries {
				if compilerVersion.MatchString(entry.Name()) {
					versions = append(versions, entry.Name())
				}
			}
		}
		for _, v := range versions {
			file := filepath.Join(dir, v, "solc-"+v)
			info, err := os.Stat(file)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if version != "" {
				return file
			}
			canonical, err := filepath.EvalSymlinks(file)
			if err != nil || seen[canonical] {
				continue
			}
			seen[canonical] = true
			matches = append(matches, file)
		}
	}
	if len(matches) == 1 {
		return matches[0]
	}
	return ""
}
