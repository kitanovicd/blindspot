package mutation

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type foundryConfig struct {
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
	FSPermissions   []fsPermission `json:"fs_permissions"`
	Fuzz            corpusConfig   `json:"fuzz"`
	Invariant       corpusConfig   `json:"invariant"`
}

type corpusConfig struct {
	FailurePersistDir string `json:"failure_persist_dir"`
}

type fsPermission struct {
	Access string `json:"access"`
	Path   string `json:"path"`
}

func workerEnv(cfg foundryConfig, root, worker string) map[string]string {
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
	permissions := []fsPermission{}
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

func loadFoundry(ctx context.Context, root, logs, profile string) (foundryConfig, error) {
	r := execute(ctx, "forge", []string{"config", "--json", "--root", root}, root, logs, 30*time.Second, profile, nil)
	var cfg foundryConfig
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

func rebase(value, original, worker string) string {
	if filepath.IsAbs(value) && inside(original, value) {
		rel, _ := filepath.Rel(original, value)
		return filepath.Join(worker, rel)
	}
	return value
}

func remappings(cfg foundryConfig, root, worker string) ([]string, error) {
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

func forgeArgs(command, worker, original, solc string, cfg foundryConfig, seed uint64) ([]string, error) {
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
