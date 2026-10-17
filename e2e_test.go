package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEndToEnd(t *testing.T) {
	if os.Getenv("BLINDSPOT_E2E") != "1" {
		t.Skip("set BLINDSPOT_E2E=1 to run real Gambit and Foundry integration tests")
	}
	cwd, _ := os.Getwd()
	binary := filepath.Join(cwd, "blindspot")
	solc := filepath.Join(cwd, ".tools", "solc-0.8.28")
	project := filepath.Join(t.TempDir(), "project with spaces")
	if err := copyTree(filepath.Join(cwd, "examples", "vault"), project, []string{"out", "cache"}); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(filepath.Join(project, "src", "Vault.sol"))
	run := func(want int, args ...string) *Run {
		t.Helper()
		cmd := exec.Command(binary, append([]string{"run", "--project", project, "--solc", solc}, args...)...)
		out, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			if e, ok := err.(*exec.ExitError); ok {
				code = e.ExitCode()
			} else {
				t.Fatal(err)
			}
		}
		if code != want {
			t.Fatalf("exit %d, want %d\n%s", code, want, out)
		}
		r, _, e := loadRun(project, "")
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	r := run(0, "--all", "--jobs", "2")
	if r.Summary.Survived == 0 || r.Summary.Caught == 0 || r.Summary.Invalid == 0 || r.Summary.Error+r.Summary.Timeout != 0 {
		t.Fatalf("unexpected real results: %+v", r.Summary)
	}
	feeGap := false
	var replayID string
	for _, m := range r.Mutations {
		if m.Line == 23 && m.Status == "survived" {
			feeGap = true
			replayID = m.ID
		}
	}
	if !feeGap {
		t.Fatal("did not identify the deliberately untested fee function")
	}
	after, _ := os.ReadFile(filepath.Join(project, "src", "Vault.sol"))
	if !bytes.Equal(original, after) {
		t.Fatal("source changed")
	}
	cmd := exec.Command(binary, "replay", replayID, "--project", project)
	output, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "SURVIVED") {
		t.Fatalf("replay failed: %v %s", err, output)
	}
	t.Logf("Initial run: %d caught, %d survived, %d invalid; source unchanged; replay passed", r.Summary.Caught, r.Summary.Survived, r.Summary.Invalid)

	// Add tests ONLY to the disposable fixture to prove that stronger assertions change the report.
	strong := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;
import {Vault} from "../src/Vault.sol";
contract AdditionalTest {
 function test_Fee() public { Vault v = new Vault(); require(v.previewFee(500) == 5, "fee"); }
 function test_ZeroDeposit() public { Vault v = new Vault(); (bool ok,) = address(v).call(abi.encodeCall(v.deposit, (0))); require(!ok, "zero deposit must revert"); }
}`
	if err = os.WriteFile(filepath.Join(project, "test", "Additional.t.sol"), []byte(strong), 0600); err != nil {
		t.Fatal(err)
	}
	strongRun := run(0, "--all", "--jobs", "2")
	for _, m := range strongRun.Mutations {
		if (m.Line == 23 || m.Line == 10) && m.Status == "survived" {
			t.Fatalf("new assertions failed to eliminate a known gap: %+v", m)
		}
	}
	if strongRun.Summary.Survived >= r.Summary.Survived {
		t.Fatal("stronger tests did not reduce survivors")
	}
	t.Logf("Stronger fixture tests reduced survivors from %d to %d", r.Summary.Survived, strongRun.Summary.Survived)

	sampled := run(0, "--limit", "4", "--seed", "9")
	repeat := run(0, "--limit", "4", "--seed", "9")
	ids := func(r *Run) string {
		var ids []string
		for _, m := range r.Mutations {
			ids = append(ids, m.ID)
		}
		return strings.Join(ids, ",")
	}
	if ids(sampled) != ids(repeat) || len(sampled.Mutations) != 4 {
		t.Fatal("sampling is not deterministic")
	}
	line := run(0, "src/Vault.sol", "--line", "23", "--all")
	for _, m := range line.Mutations {
		if m.Line != 23 {
			t.Fatal("line filter leaked")
		}
	}
	data, _ := json.Marshal(line)
	if !bytes.Contains(data, []byte(`"schema_version":1`)) {
		t.Fatal("JSON contract missing")
	}

	if err = os.Remove(filepath.Join(project, "test", "Additional.t.sol")); err != nil {
		t.Fatal(err)
	}
	run(2, "src/Vault.sol", "--line", "23", "--all", "--fail-on-survivors")
	broken := `pragma solidity ^0.8.28; contract BrokenTest { function test_Broken() public pure { require(false, "deliberate baseline failure"); } }`
	_ = os.WriteFile(filepath.Join(project, "test", "Broken.t.sol"), []byte(broken), 0600)
	failed := run(1, "--limit", "1")
	if failed.State != "failed" || len(failed.Mutations) != 0 {
		t.Fatal("baseline failure did not stop generation")
	}
	_ = os.Remove(filepath.Join(project, "test", "Broken.t.sol"))
	config, _ := os.ReadFile(filepath.Join(project, "foundry.toml"))
	config = bytes.Replace(config, []byte("[profile.default]"), []byte("[profile.default]\nmatch_test = 'NONEXISTENT_TEST'"), 1)
	_ = os.WriteFile(filepath.Join(project, "foundry.toml"), config, 0600)
	failed = run(1, "--limit", "1")
	if failed.Baseline.Tests != 0 {
		t.Fatal("zero-test scenario did not execute")
	}
	t.Log("Verified deterministic sampling, line filtering, CI exit codes, failed baseline, and zero-test rejection")
}

func TestEndToEndRemappingsProfileAndFuzz(t *testing.T) {
	if os.Getenv("BLINDSPOT_E2E") != "1" {
		t.Skip("set BLINDSPOT_E2E=1")
	}
	cwd, _ := os.Getwd()
	root := t.TempDir()
	files := map[string]string{
		"foundry.toml": `[profile.default]
src = "contracts"
test = "checks"
out = "artifacts"
cache_path = "compile-cache"
libs = ["vendor"]
remappings = ["@math/=vendor/math/"]
solc_version = "0.8.28"
via_ir = true
[profile.mutation]
optimizer = true
optimizer_runs = 300
[profile.mutation.fuzz]
runs = 16
`,
		"vendor/math/Math.sol":     `pragma solidity ^0.8.28; library Math { function double(uint256 a) internal pure returns(uint256) { return a * 2; } }`,
		"contracts/Calculator.sol": `pragma solidity ^0.8.28; import {Math} from "@math/Math.sol"; contract Calculator { function quote(uint64 amount) external pure returns(uint256) { return Math.double(amount) + 1; } }`,
		"checks/Calculator.t.sol":  `pragma solidity ^0.8.28; import {Calculator} from "../contracts/Calculator.sol"; contract CalculatorTest { Calculator c; function setUp() public { c = new Calculator(); } function testFuzz_Quote(uint64 a) public view { require(c.quote(a) == uint256(a) * 2 + 1, "quote"); } }`,
	}
	for file, data := range files {
		p := filepath.Join(root, file)
		_ = os.MkdirAll(filepath.Dir(p), 0700)
		if err := os.WriteFile(p, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(filepath.Join(cwd, "blindspot"), "run", "--project", root, "--profile", "mutation", "--all", "--json")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("remapping/profile/fuzz run: %v\n%s\n%s", err, stderr.String(), string(out))
	}
	var run Run
	if err = json.Unmarshal(out, &run); err != nil {
		t.Fatal(err)
	}
	if run.Profile != "mutation" || run.Summary.Caught == 0 || run.Summary.Error+run.Summary.Timeout != 0 || run.Baseline.Tests != 1 {
		t.Fatalf("unexpected integration result: %+v", run)
	}
	if exists(filepath.Join(root, "artifacts")) || exists(filepath.Join(root, "compile-cache")) {
		t.Fatal("original build outputs were modified")
	}
	t.Logf("Custom paths, remapping, via-IR, named profile and real fuzz test passed: %+v", run.Summary)
}

func TestEndToEndFFI(t *testing.T) {
	if os.Getenv("BLINDSPOT_E2E") != "1" {
		t.Skip("set BLINDSPOT_E2E=1")
	}
	cwd, _ := os.Getwd()
	binary := filepath.Join(cwd, "blindspot")
	root := filepath.Join(t.TempDir(), "ffi project")
	if err := copyTree(filepath.Join(cwd, "examples", "vault"), root, []string{"out", "cache"}); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "foundry.toml")
	original, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	config := bytes.Replace(original, []byte("[profile.default]"), []byte("[profile.default]\nffi = true"), 1)
	if err := os.WriteFile(configPath, config, 0600); err != nil {
		t.Fatal(err)
	}
	ffiTest := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;
import {Vault} from "../src/Vault.sol";
interface FFIVm { function ffi(string[] calldata) external returns (bytes memory); }
contract FFIOracleTest {
    FFIVm constant vm = FFIVm(address(uint160(uint256(keccak256("hevm cheat code")))));
    function test_FeeFromFFI() public {
        string[] memory command = new string[](3);
        command[0] = "/bin/sh";
        command[1] = "-c";
        command[2] = "pwd > .ffi-ran; printf 0x05";
        bytes memory expected = vm.ffi(command);
        require(expected.length == 1, "FFI output");
        Vault v = new Vault();
        require(v.previewFee(500) == uint8(expected[0]), "fee from external oracle");
    }
}`
	if err := os.WriteFile(filepath.Join(root, "test", "FFI.t.sol"), []byte(ffiTest), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"run", "--project", root, "--line", "23", "--all", "--json"}
	out, err := exec.Command(binary, append(args, "src/Vault.sol")...).CombinedOutput()
	if err == nil || !bytes.Contains(out, []byte("Re-run with --allow-ffi")) {
		t.Fatalf("expected actionable FFI rejection: %v\n%s", err, out)
	}
	if exists(filepath.Join(root, ".blindspot", "latest.json")) || exists(filepath.Join(root, ".ffi-ran")) {
		t.Fatal("rejected run executed tests")
	}
	out, err = exec.Command(binary, "doctor", "--project", root).CombinedOutput()
	if err != nil || !bytes.Contains(out, []byte("add --allow-ffi")) {
		t.Fatalf("doctor should inspect without executing FFI: %v\n%s", err, out)
	}
	// Put the boolean flag before the positional target to exercise argument parsing.
	cmd := exec.Command(binary, append(args, "--allow-ffi", "src/Vault.sol")...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err = cmd.Output()
	if err != nil {
		t.Fatalf("FFI run: %v\n%s\n%s", err, stderr.String(), out)
	}
	var run Run
	if err := json.Unmarshal(out, &run); err != nil {
		t.Fatal(err)
	}
	if !run.FFI || run.Baseline.Tests != 4 || run.Summary.Caught == 0 || run.Summary.Survived+run.Summary.Error+run.Summary.Timeout != 0 {
		t.Fatalf("unexpected FFI results: %+v", run)
	}
	if exists(filepath.Join(root, ".ffi-ran")) {
		t.Fatal("FFI used the original project as its working directory")
	}
	after, _ := os.ReadFile(configPath)
	if !bytes.Equal(config, after) {
		t.Fatal("original Foundry configuration changed")
	}
	dir := filepath.Join(root, ".blindspot", "runs", run.ID)
	marker, err := os.ReadFile(filepath.Join(dir, "snapshot", ".ffi-ran"))
	if err != nil || !strings.Contains(string(marker), "/.blindspot/runs/"+run.ID+"/snapshot") {
		t.Fatalf("FFI did not execute in the saved snapshot: %v %s", err, marker)
	}
	// Replay must use the recorded, permitted setting even if the caller's env changes.
	t.Setenv("FOUNDRY_FFI", "false")
	for _, m := range run.Mutations {
		if m.Status != "caught" {
			continue
		}
		out, err := exec.Command(binary, "replay", m.ID, "--project", root).CombinedOutput()
		if err != nil || !bytes.Contains(out, []byte("CAUGHT")) || !bytes.Contains(out, []byte("saved --allow-ffi run")) {
			t.Fatalf("FFI replay failed: %v\n%s", err, out)
		}
		break
	}
	// Permission does not turn FFI on when the effective configuration disables it.
	if err := os.WriteFile(configPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	out, err = exec.Command(binary, append(args, "--allow-ffi", "src/Vault.sol")...).CombinedOutput()
	if err == nil || !bytes.Contains(out, []byte("original tests must pass")) {
		t.Fatalf("--allow-ffi enabled FFI despite disabled configuration: %v\n%s", err, out)
	}
	failed, _, err := loadRun(root, "")
	if err != nil || failed.FFI || len(failed.Mutations) != 0 {
		t.Fatalf("disabled FFI run was not stopped at baseline: %v %+v", err, failed)
	}
	t.Logf("FFI opt-in, original files preserved, real subprocess tests (%d caught), doctor and replay verified", run.Summary.Caught)
}

func TestEndToEndInterruptSavesProgress(t *testing.T) {
	if os.Getenv("BLINDSPOT_E2E") != "1" {
		t.Skip("set BLINDSPOT_E2E=1")
	}
	cwd, _ := os.Getwd()
	project := filepath.Join(t.TempDir(), "project")
	if err := copyTree(filepath.Join(cwd, "examples", "vault"), project, []string{"out", "cache"}); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(cwd, "blindspot"), "run", "--project", project, "--all", "--jobs", "1")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill() }()
	ready := make(chan bool, 1)
	go func() {
		scan := bufio.NewScanner(stderr)
		sent := false
		for scan.Scan() {
			if !sent && strings.Contains(scan.Text(), "Evaluate  ") {
				ready <- true
				sent = true
			}
		}
		if !sent {
			ready <- false
		}
	}()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("process ended before evaluating mutants")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("run did not reach mutation evaluation")
	}
	if err = cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 130 {
		t.Fatalf("unexpected interruption exit: %v", err)
	}
	r, dir, err := loadRun(project, "")
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "interrupted" || r.Summary.NotRun == 0 || !exists(filepath.Join(dir, "report.html")) {
		t.Fatalf("interrupted report missing: %+v", r)
	}
	t.Logf("Interrupted run preserved %d pending mutants and a readable report", r.Summary.NotRun)
}
