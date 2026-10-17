package toolchain

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Locate resolves an explicit tool, PATH entry, or executable-local installation.
func Locate(explicit, name string) (string, error) {
	if explicit != "" {
		p, err := filepath.Abs(explicit)
		if err != nil {
			return "", err
		}
		if exists(p) {
			return p, nil
		}
		if p, err := exec.LookPath(explicit); err == nil {
			return p, nil
		}
		return "", fmt.Errorf("executable not found: %s", explicit)
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	exe, _ := os.Executable()
	for _, dir := range []string{filepath.Join(filepath.Dir(exe), ".tools"), filepath.Join(filepath.Dir(filepath.Dir(exe)), ".tools")} {
		p := filepath.Join(dir, name)
		if exists(p) {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s is missing. Run `blindspot setup` or pass --%s /path/to/%s", name, name, name)
}

// ResolveSolc selects one native compiler matching the project's configuration.
func ResolveSolc(explicit, configured, root string) (string, error) {
	if explicit != "" {
		return Locate(explicit, "solc")
	}
	version := strings.TrimPrefix(configured, "solc:")
	if version != "" && !compilerVersion.MatchString(version) {
		if !filepath.IsAbs(version) {
			version = filepath.Join(root, version)
		}
		return Locate(version, "solc")
	}
	exe, _ := os.Executable()
	home, _ := os.UserHomeDir()
	svmDirs := svmDirectories(home, runtime.GOOS, os.Getenv("XDG_DATA_HOME"), os.Getenv("APPDATA"))
	if version != "" {
		for _, p := range []string{filepath.Join(filepath.Dir(exe), ".tools", "solc-"+version), filepath.Join(filepath.Dir(filepath.Dir(exe)), ".tools", "solc-"+version), filepath.Join(root, ".blindspot", "tools", "solc-"+version)} {
			if exists(p) {
				return p, nil
			}
		}
		if p := findSVMCompiler(svmDirs, version); p != "" {
			return p, nil
		}
	}
	if p, err := exec.LookPath("solc"); err == nil {
		out, _ := exec.Command(p, "--version").Output()
		if version == "" || strings.Contains(string(out), "Version: "+version+"+") {
			return p, nil
		}
	}
	if version == "" {
		if p := findSVMCompiler(svmDirs, ""); p != "" {
			return p, nil
		}
	}
	return "", fmt.Errorf("could not resolve a single native Solidity compiler (configured version: %q). Pass --solc /path/to/solc. Checked Blindspot tools, PATH, and Foundry compiler directories: %s", configured, strings.Join(svmDirs, ", "))
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }
