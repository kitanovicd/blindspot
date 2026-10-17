# Blindspot

Source-first mutation coverage for **Foundry**, powered by **Certora Gambit**.

Blindspot runs your existing tests against independently mutated copies of your Solidity source. It produces a local browser report highlighting source lines where changes survived. It does not generate, modify, or repair tests.

The CLI is Go. The HTML/CSS/JavaScript report is embedded in the executable and works offline. No Node, npm, frontend build, account, or hosted service is required.

## Try the included demo

Install Go 1.23+ and Foundry (`forge` on PATH), then run these commands from the repository root. Compiled binaries, local toolchains, and saved reports are not included in the source repository.

```sh
make build
./blindspot setup
forge build --root examples/vault
./blindspot run --project examples/vault --all --open
```

The example is deliberately under-tested. An initial full run produces 72 mutants: 38 caught, 10 surviving, and 24 invalid, with three source lines showing potential gaps. Invalid mutations are excluded from evaluated coverage. Runtime on the development machine was approximately ten seconds.

The initial Forge build installs the demo's Solidity 0.8.28 compiler if needed. Blindspot finds Foundry's installed compiler automatically.

After a run, reopen the saved result without rerunning tests:

```sh
./blindspot report --project examples/vault
```

## Use your Foundry project

```sh
./blindspot doctor --project /path/to/project
./blindspot run --project /path/to/project --open
./blindspot run --project /path/to/project src/Vault.sol --limit 30
./blindspot run --project /path/to/project src/Vault.sol --line 42 --all
./blindspot run --project /path/to/project --changed origin/main
```

Targets are relative to the project root. File paths, directories, and shell-style path patterns are supported. Quote patterns so your shell does not expand them relative to the wrong directory. `--changed` selects changed **files**, including staged, unstaged, and untracked files; it still runs the full configured test suite. It does not attempt dependency-based test selection.

The default is a deterministic sample of up to 50 mutants. `--all` evaluates all mutations Gambit generates within the selected source scope. Unsupported syntax and unmutated locations remain unassessed. One line can have multiple mutations.

Common options:

```sh
--profile mutation       # Uses this Foundry profile; defaults to FOUNDRY_PROFILE/default
--allow-ffi              # Permit subprocesses if your Foundry profile enables FFI
--jobs 2                 # Independent workers, one Forge thread each
--timeout 120            # Seconds allowed per test process, after compilation
--build-timeout 600      # Seconds allowed per compilation (baseline and mutants)
--seed 42                # Gambit sampling and Forge fuzz seed
--solc /path/to/solc     # Override native Solidity compiler
--gambit /path/to/gambit # Override Gambit executable
--json                   # Final result JSON on stdout, progress on stderr
--fail-on-survivors      # CI exits 2 when mutations survive
```

Existing Foundry test filters, fuzz/invariant settings, optimizer settings, via-IR, remappings, and the active profile are preserved. The baseline and each mutant use the same selected test suite and seed. Foundry performs authoritative compilation. Gambit's own mutant validation is disabled so its narrower compiler interface does not decide whether a mutant compiles under Foundry.

Blindspot requests minimal Forge verbosity for machine-readable results, so project verbosity settings do not produce large call traces for every mutant. Test selection and assertions are unchanged. It parses the complete JSON log one suite at a time; bounded log excerpts are used only for diagnostics. Malformed JSON and a valid result with zero tests produce distinct errors.

The baseline compiles the original project first, then runs its tests with a separate timer. Compilation defaults to the larger of 600 seconds and `--timeout`; `--build-timeout` explicitly overrides it. Test execution defaults to 120 seconds. Both baseline phases print elapsed time every 15 seconds and save their durations separately. A timeout stops the baseline and reports that mutation coverage has not been evaluated; it is not evidence of failed assertions. Compilation and test logs are saved under `logs/baseline/build` and `logs/baseline/test`. Replay uses the saved limits and accepts both timeout overrides.

## Read the report

The report is a plain file review. It starts with surviving mutations grouped by file and original source line, with a short source excerpt. There are no charts or dashboard cards.

- **Survived:** changes that passed your tests; the affected source lines are highlighted in red.
- **Unresolved:** timeouts, execution errors, or interrupted mutations. Shown when there is unfinished work.
- **All:** every generated result in this run, including caught and invalid mutants.

Expand **Show mutations** under a line to inspect the before/after changes or copy a replay command. Full diffs, logs, full source, and run metadata are collapsed until needed. File search filters the results. The editor link opens the original source location in VS Code. Copied replay commands include the executable's full path.

Invalid mutations never count as caught. A process with zero executed tests is an error, not a survivor. A baseline that fails or times out shows no mutation results.

**Mutation coverage is not line execution coverage.** A surviving mutation is a potential gap; some mutations preserve behavior. Unassessed lines are not proven covered. Blindspot does not classify semantic equivalence.

After updating Blindspot, `blindspot report --project /path/to/project` refreshes the saved report with the current layout without rerunning tests.

## Reports, replay, and CI

```sh
./blindspot report --project /path/to/project --serve
./blindspot report --project /path/to/project --export report.html --no-open
./blindspot replay m-0123456789ab --project /path/to/project --run RUN_ID
./blindspot run --project /path/to/project --fail-on-survivors --json > result.json
```

The optional server binds only to `127.0.0.1` and serves the report, not the project directory. HTML exports are self-contained and include selected Solidity source, mutation evidence, and absolute local paths. Saved run directories also contain project snapshots and subprocess logs, which can include local configuration or secrets. Keep these artifacts out of public repositories. This repository ignores `.blindspot/` and the default export filenames; add `.blindspot/` to the ignore rules of any project you test.

Every run is saved under `PROJECT/.blindspot/runs/RUN_ID/`:

```text
run.json       Versioned structured result, source snapshot, and line locations
report.html    Self-contained browser report
snapshot/      Original project files used by this run
generated/     Gambit configs, metadata, and original mutant artifacts
logs/          Baseline, generation, compilation, and test logs
replay-logs/   Separate evidence from subsequent replays
```

Replay uses the saved source/test snapshot and recorded tool paths, profile, seed, and FFI setting, including the permission from an original `--allow-ffi` run. It rechecks the original baseline in the current environment before testing the mutant. External environment variables and RPC responses are not frozen; replay requires the original compiler to remain available. To test new source/tests, start a new run.

Ctrl+C kills active process groups and preserves completed results with pending mutations marked as not run. Results are checkpointed after each mutation. This release supports replay of individual mutants; automatic continuation of an interrupted batch is not implemented. Completed worker directories are removed. Saved snapshots are retained for replay and can be removed by deleting old run directories.

Exit codes:

| Code | Meaning |
| --- | --- |
| 0 | Evaluation completed (survivors are informational by default) |
| 1 | Setup, configuration, baseline, or generation failure |
| 2 | Survivors found with `--fail-on-survivors` |
| 3 | Incomplete evaluation: timeouts, runner errors, or unfinished work |
| 130 | Interrupted |

## Toolchain and compatibility

Required: Foundry on PATH, Gambit v1.0.6, and a compatible native `solc`.

`blindspot setup` downloads the pinned Gambit release beside the executable and verifies a pinned SHA-256 digest. It supports macOS (universal binary) and Linux x86-64. Linux ARM requires a manually built Gambit. Windows users should use WSL. Tested on macOS ARM64; Linux support is cross-compiled, not runtime-tested here.

Compiler resolution uses `--solc`, your configured compiler path/version, local managed binaries, Foundry's installed compilers, or a matching `solc` on PATH. Blindspot searches both `~/.svm` and the platform data directory: `~/Library/Application Support/svm` on macOS, or `$XDG_DATA_HOME/svm` (default `~/.local/share/svm`) on Linux. These follow [SVM's upstream directory layout](https://github.com/alloy-rs/svm-rs/blob/master/crates/svm-rs/src/paths.rs). Projects without a pinned compiler need a single unambiguous installed compiler or `--solc`. Blindspot does not silently install or upgrade Foundry or global compilers.

This release supports one compiler per run. Mixed-compiler projects need separate compatible targets/configurations. Source, dependencies, output paths, and filesystem permissions must stay inside the project root. Internal symlinks are materialized into the snapshot; external symlinks are rejected. For monorepos, use a self-contained Foundry root. Gambit generation failures are surfaced as incomplete scope instead of claiming those files are covered.

Workers are project copies, not OS security sandboxes. If your Foundry profile has `ffi = true`, run `blindspot run --allow-ffi` to permit FFI subprocesses. This flag preserves the configured setting; it does not enable FFI for profiles that disable it. FFI runs during the baseline and each mutant's tests, with the project copy as its working directory, but commands can still access files and services outside that copy. Helpers must tolerate repeated execution; use `--jobs 1` if they share external state. Blindspot records the FFI setting for replay and does not edit your `foundry.toml`. `blindspot doctor` reports the setting without running tests.

Forks configured by RPC URL require a pinned block. Tests that select forks programmatically must pin those blocks themselves. Arbitrary external services, time-dependent tests, or tests with filesystem side effects can still be nondeterministic; use a deterministic test profile. Persistent fuzz and invariant failure corpora are reset to baseline between mutants so one mutant's failure cannot contaminate the next.

## Build and test

Go 1.23+; only the Go standard library is used.

```sh
go build -trimpath -o blindspot .
go test -race ./...
BLINDSPOT_E2E=1 go test -race -v ./...
```

`make build`, `make test`, and `make integration` use Go from PATH, falling back to `.tools/go/bin/go` if installed locally. Integration tests require a native Solidity 0.8.28 compiler at `.tools/solc-0.8.28`, Gambit, and Foundry. These binaries must be installed separately. The tests create disposable example projects.

Tests cover real mutation detection, survivor replay, source preservation, stronger assertions eliminating known gaps, deterministic sampling, line filtering, CLI JSON, CI exit codes, baseline/zero-test rejection, custom source/output paths, remappings, via-IR, a named profile, real fuzz tests, explicit FFI permission and replay, process timeout/cancellation, symlink isolation, report escaping, and fuzz corpus reset.

The core boundaries are `project.go` (Foundry configuration and isolation), `engine.go` (Gambit and scheduling), `process.go` (process management and test evidence), and `report.go`/`ui/report.html` (local report). There is no alternate mutation engine: every evaluated source mutation originates in Gambit.

## Upstream

- [Certora Gambit](https://github.com/Certora/gambit) — MIT license
- [Foundry](https://github.com/foundry-rs/foundry)
- [Solidity](https://github.com/argotorg/solidity)

Blindspot is independent of those projects. See `THIRD_PARTY_NOTICES.md` for bundled tool information.
