package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

type FoundryConfig struct {
	Src             string         `json:"src"`
	Test            string         `json:"test"`
	Script          string         `json:"script"`
	Out             string         `json:"out"`
	CachePath       string         `json:"cache_path"`
	Libs            []string       `json:"libs"`
	Remappings      []string       `json:"remappings"`
	Solc            string         `json:"solc"`
	EVMVersion      string         `json:"evm_version"`
	Optimizer       bool           `json:"optimizer"`
	ViaIR           bool           `json:"via_ir"`
	FFI             bool           `json:"ffi"`
	EthRPCURL       string         `json:"eth_rpc_url"`
	ForkBlockNumber *uint64        `json:"fork_block_number"`
	FSPermissions   []FSPermission `json:"fs_permissions"`
	Fuzz            CorpusConfig   `json:"fuzz"`
	Invariant       CorpusConfig   `json:"invariant"`
}

type CorpusConfig struct {
	FailurePersistDir string `json:"failure_persist_dir"`
}

type FSPermission struct {
	Access string `json:"access"`
	Path   string `json:"path"`
}

func workerEnv(cfg FoundryConfig, root, worker string) map[string]string {
	// Keep the inspected FFI setting consistent across baseline, workers and replay.
	env := map[string]string{
		"FOUNDRY_FFI": fmt.Sprint(cfg.FFI),
		// Avoid serializing large call traces for every mutant. This controls
		// output verbosity, not which tests or assertions execute.
		"FOUNDRY_VERBOSITY": "0",
	}
	if cfg.Test != "" {
		env["FOUNDRY_TEST"] = rebase(cfg.Test, root, worker)
	}
	if cfg.Script != "" {
		env["FOUNDRY_SCRIPT"] = rebase(cfg.Script, root, worker)
	}
	libs := []string{}
	for _, p := range cfg.Libs {
		libs = append(libs, rebase(p, root, worker))
	}
	data, _ := json.Marshal(libs)
	env["FOUNDRY_LIBS"] = string(data)
	permissions := []FSPermission{}
	for _, p := range cfg.FSPermissions {
		p.Path = rebase(p.Path, root, worker)
		permissions = append(permissions, p)
	}
	data, _ = json.Marshal(permissions)
	env["FOUNDRY_FS_PERMISSIONS"] = string(data)
	env["FOUNDRY_FUZZ_FAILURE_PERSIST_DIR"] = filepath.Join(worker, ".blindspot-state", "fuzz")
	env["FOUNDRY_INVARIANT_FAILURE_PERSIST_DIR"] = filepath.Join(worker, ".blindspot-state", "invariant")
	env["FOUNDRY_TEST_FAILURES_FILE"] = filepath.Join(worker, ".blindspot-state", "test-failures")
	return env
}

func seedCorpus(cfg FoundryConfig, root, snapshot string) error {
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

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func exists(file string) bool   { _, err := os.Stat(file); return err == nil }
func inside(root, file string) bool {
	rel, err := filepath.Rel(root, file)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}
func writeJSON(file string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		return err
	}
	if err = os.WriteFile(file+".tmp", data, 0600); err != nil {
		return err
	}
	return os.Rename(file+".tmp", file)
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

func loadFoundry(ctx context.Context, root, logs, profile string) (FoundryConfig, error) {
	r := execute(ctx, "forge", []string{"config", "--json", "--root", root}, root, logs, 30*time.Second, profile, nil)
	var cfg FoundryConfig
	if r.Code != 0 {
		return cfg, fmt.Errorf("could not read Foundry configuration: %s %v", r.Stderr, r.Err)
	}
	if err := parseJSON(r.Stdout, &cfg); err != nil {
		return cfg, fmt.Errorf("could not decode forge config: %w", err)
	}
	for _, p := range append([]string{cfg.Src, cfg.Test, cfg.Script, cfg.Out, cfg.CachePath}, cfg.Libs...) {
		if p == "" {
			continue
		}
		full := p
		if !filepath.IsAbs(p) {
			full = filepath.Join(root, p)
		}
		if !inside(root, filepath.Clean(full)) {
			return cfg, fmt.Errorf("path outside project root is not supported yet: %s. Use a project-local source, dependency, and output layout", p)
		}
	}
	for _, p := range cfg.FSPermissions {
		full := p.Path
		if !filepath.IsAbs(full) {
			full = filepath.Join(root, full)
		}
		if !inside(root, filepath.Clean(full)) {
			return cfg, fmt.Errorf("filesystem permission escapes project root: %s. Use project-local permissions for mutation runs", p.Path)
		}
	}
	if cfg.EthRPCURL != "" && cfg.ForkBlockNumber == nil {
		return cfg, fmt.Errorf("fork tests require a pinned fork_block_number for a comparable baseline")
	}
	return cfg, nil
}

func locateTool(explicit, name string) (string, error) {
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

func resolveSolc(explicit string, cfg FoundryConfig, root string) (string, error) {
	if explicit != "" {
		return locateTool(explicit, "solc")
	}
	version := strings.TrimPrefix(cfg.Solc, "solc:")
	if version != "" && !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(version) {
		if !filepath.IsAbs(version) {
			version = filepath.Join(root, version)
		}
		return locateTool(version, "solc")
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
	return "", fmt.Errorf("could not resolve a single native Solidity compiler (configured version: %q). Pass --solc /path/to/solc. Checked Blindspot tools, PATH, and Foundry compiler directories: %s", cfg.Solc, strings.Join(svmDirs, ", "))
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

func rebase(value, original, worker string) string {
	if filepath.IsAbs(value) && inside(original, value) {
		rel, _ := filepath.Rel(original, value)
		return filepath.Join(worker, rel)
	}
	return value
}

func remappings(cfg FoundryConfig, root, worker string) ([]string, error) {
	var result []string
	for _, mapping := range cfg.Remappings {
		prefix, target, ok := strings.Cut(mapping, "=")
		if !ok {
			return nil, fmt.Errorf("invalid remapping: %s", mapping)
		}
		if filepath.IsAbs(target) && !inside(root, filepath.Clean(target)) {
			return nil, fmt.Errorf("external dependency path is not supported: %s", target)
		}
		if !filepath.IsAbs(target) && !inside(root, filepath.Join(root, target)) {
			return nil, fmt.Errorf("dependency escapes project root: %s", target)
		}
		result = append(result, prefix+"="+rebase(target, root, worker))
	}
	return result, nil
}

func forgeArgs(command, worker, original, solc string, cfg FoundryConfig, seed uint64) ([]string, error) {
	out := cfg.Out
	if out == "" {
		out = "out"
	}
	cache := cfg.CachePath
	if cache == "" {
		cache = "cache"
	}
	out = rebase(out, original, worker)
	cache = rebase(cache, original, worker)
	if !filepath.IsAbs(out) {
		out = filepath.Join(worker, out)
	}
	if !filepath.IsAbs(cache) {
		cache = filepath.Join(worker, cache)
	}
	args := []string{command, "--root", worker, "--use", solc, "--out", out, "--cache-path", cache}
	if cfg.Src != "" {
		args = append(args, "--contracts", rebase(cfg.Src, original, worker))
	}
	maps, err := remappings(cfg, original, worker)
	if err != nil {
		return nil, err
	}
	for _, m := range maps {
		args = append(args, "--remappings", m)
	}
	if command == "test" {
		args = append(args, "--json", "--threads", "1", "--fuzz-seed", fmt.Sprint(seed))
	}
	return args, nil
}
