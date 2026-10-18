# Usage guide

See the [README](../README.md) for installation and a first run.

## Installation options

With Homebrew, use `brew install kitanovicd/tap/blindspot`, then `blindspot`. Homebrew installs the binary on your PATH and verifies its release checksum. Update with `brew update && brew upgrade blindspot`; remove it with `brew uninstall blindspot`. Uninstalling keeps saved project reports and the Gambit cache.

The standalone installer uses `~/.local/bin` and never changes your shell configuration or requires sudo. Run it again to update, or choose a directory and version:

```sh
curl -fsSL https://raw.githubusercontent.com/kitanovicd/blindspot/main/install.sh | \
  BLINDSPOT_INSTALL_DIR="$HOME/.local/bin" BLINDSPOT_VERSION=v0.1.0 sh
```

For a manual installation, download the archive for your platform and `checksums.txt` from the [release page](https://github.com/kitanovicd/blindspot/releases/latest). Verify the archive's SHA-256 hash, extract it, and put `blindspot` in a directory on your PATH.

Go users can install without the shell installer:

```sh
go install github.com/kitanovicd/blindspot@latest
export PATH="$(go env GOPATH)/bin:$PATH"
blindspot setup
```

If you set `GOBIN`, use that directory on PATH instead. `blindspot version` prints the installed version.

`blindspot setup` downloads Gambit v1.0.6 and checks its pinned SHA-256 hash. Managed tools live in `~/Library/Caches/blindspot/tools` on macOS and `$XDG_CACHE_HOME/blindspot/tools` (usually `~/.cache/blindspot/tools`) on Linux. Set `BLINDSPOT_TOOLS_DIR` to override this location; keep it set when running Blindspot. If you clear the cache, run `blindspot setup` again.

An explicit `--gambit` takes precedence, followed by the managed installation, your PATH, and `.tools` beside the executable (for source checkouts). Install Foundry separately. Guided setup builds the project before compiler discovery so Forge can install the required Solidity compiler. Use `blindspot doctor --project /path/to/project` to inspect an existing setup.

To uninstall the default installation, remove `~/.local/bin/blindspot` and, optionally, the Blindspot tools cache. Saved reports stay in each project's `.blindspot/` directory.

## Guided setup and custom commands

Run `blindspot` in a terminal to choose between running mutation tests, freeing disk space, and opening a saved report. Arrow keys move through the choices; Enter selects and Esc cancels. Use bare `blindspot run` to go directly to guided setup. It asks for:

1. The Foundry project path.
2. A dependency installation command, such as `git submodule update --init --recursive`, or `skip`.
3. A build command, such as `forge build` or `make build`.
4. A test command, such as `forge test` or `make test`.
5. The source scope, mutation limit, and whether to open the report.
6. Whether to keep project copies and mutation files for replay (default: no, remembered per project).

Dependency installation and the initial build run in the original project, before reading compiler configuration or taking a snapshot. These commands can change project files. Dependencies are installed once; the build and test commands run again for the baseline and every mutation inside separate project copies. Setup and builds use `--build-timeout`; each test command uses `--timeout`. Command output is saved in the run's `logs/` directory.

Commands run through `/bin/sh` from the project root. Shell operators, Makefiles, and executable scripts work; interactive shell aliases do not. Wrappers must invoke `forge` through PATH and work without Git metadata in the copies. Blindspot supplies the compiler, copy-local paths, fuzz seed, and JSON output automatically. Test filters are preserved, and multiple Forge test invocations are supported. A successful shell exit without executed Forge tests fails the baseline. Keep the Foundry layout and compiler configuration in `foundry.toml`, and select profiles with `--profile`.

The project path can contain spaces or start with `~`. Sources, tests, dependencies, and build directories follow the project's Foundry configuration rather than a hardcoded `src/` or `test/` layout. Dependencies must still live inside the selected project root.

Commands are stored in `PROJECT/.blindspot/settings.json` and shown as prompt defaults on the next guided run. Noninteractive runs do not read saved commands. Keep secrets in environment variables rather than command text; commands are also recorded with the results for replay.

When you decline to keep replay files, Blindspot saves the HTML report and JSON results, then removes snapshots, generated mutations, and build caches. Failed and interrupted runs retain diagnostic logs. Cleanup happens only after the results are saved. Runs using flags keep artifacts by default for compatibility; pass `--keep-artifacts=false` to clean them automatically. Use `--keep-artifacts` to retain them explicitly.

Use `--interactive` to request the prompts explicitly, or pass commands directly for CI:

```sh
blindspot run --project /path/to/project --limit 5 \
  --install-command 'git submodule update --init --recursive' \
  --build-command 'make build' --test-command 'make test'
```

Omit `--install-command` to skip dependency installation. Runs with no custom command flags retain the direct Forge workflow and expect a prepared project. Bare `blindspot` prints help when stdin is not a terminal. `--interactive` and `--json` cannot be combined.

## Choose what to mutate

```sh
# One file, at most 20 mutants
blindspot run --project /path/to/project src/Vault.sol --limit 20 --open

# One original source line
blindspot run --project /path/to/project src/Vault.sol --line 42 --all

# A directory or quoted path pattern
blindspot run --project /path/to/project src/vaults/ --all
blindspot run --project /path/to/project 'src/*Vault.sol' --all

# Source files changed since a Git ref
blindspot run --project /path/to/project --changed origin/main
```

Targets are relative to the project root. Quote patterns so your shell does not expand them first. `--line` requires one source file. `--changed` selects whole files, including staged, unstaged, and untracked changes; it still runs the full configured test suite.

The default sample is up to 50 mutants. `--limit N` sets a maximum; `--all` evaluates every mutation Gambit generates in the selected scope. One line may have several mutations, and unsupported or unmutated locations remain unassessed.

## Run options

| Option | Purpose and default |
| --- | --- |
| `--project PATH` | Foundry project root; current directory by default. |
| `--interactive` | Ask for project setup and mutation scope. |
| `--install-command CMD` | Dependency installation; runs once in the original project. |
| `--build-command CMD` | Build during setup, baseline, mutations, and replay; default Forge. |
| `--test-command CMD` | Tests during baseline, mutations, and replay; default Forge. |
| `--keep-artifacts=BOOL` | Retain replay files; true for noninteractive runs, guided setup asks. |
| `--profile NAME` | Foundry profile; `FOUNDRY_PROFILE` or `default`. |
| `--limit N` | Maximum mutants; 50 by default. |
| `--all` | Remove the mutant count limit. |
| `--line N` | Restrict mutations to one original line in one selected file. |
| `--changed REF` | Select source files changed since a Git ref. |
| `--jobs N` | Parallel workers; default 2, bounded by available CPUs. |
| `--timeout SECONDS` | Time allowed per test process after compilation; default 120. |
| `--build-timeout SECONDS` | Time allowed per build; default is the larger of 600 and `--timeout`. |
| `--seed N` | Mutation sampling and Forge fuzz seed; default 42. |
| `--allow-ffi` | Permit subprocesses when the selected profile enables FFI. |
| `--solc PATH` | Override the native Solidity compiler. |
| `--gambit PATH` | Override the Gambit executable. |
| `--open` | Open the report after the run. |
| `--json` | Write final JSON to stdout; progress stays on stderr. |
| `--fail-on-survivors` | Exit with code 2 when mutations survive. |

For a slow project, increase compilation and test limits independently:

```sh
blindspot run --project /path/to/project --limit 5 --build-timeout 1200 --timeout 300
```

The baseline compiles the original project, then runs its tests with a separate timer. Both phases must succeed before mutations are evaluated. A timeout or zero executed tests never counts as coverage. Logs are saved under `logs/baseline/build` and `logs/baseline/test` in the run directory.

Each worker uses its own project copy and one Forge thread. The selected profile, test filters, fuzz/invariant settings, optimizer, via-IR, and remappings are preserved. Foundry determines whether each mutant compiles; Gambit's separate mutant validation is disabled. Fuzz and invariant failure corpora are reset to baseline between mutants.

The terminal separates preparation, baseline, generation, and evaluation with headings and blank lines. Generation lists each source file with its mutation count, or an explanation if no mutations were found or generation failed. Slow files get an update every 15 seconds. During evaluation, the terminal shows completed counts, results, and elapsed time every 15 seconds, plus a final update. Individual code changes stay in the report.

Colors highlight headings, passed/caught results, survivors, and errors in a terminal. Redirected streams stay plain, and `--json` always emits unstyled JSON. Set `NO_COLOR=1` to disable colors; `TERM=dumb` also disables them.

## Reports and saved runs

```sh
# Latest report, or a specific run
blindspot report --project /path/to/project
blindspot report --project /path/to/project --run RUN_ID

# Export an offline HTML file
blindspot report --project /path/to/project --export report.html --no-open

# Serve the report on localhost instead of opening a file
blindspot report --project /path/to/project --serve --port 8080
```

Reports are self-contained. The optional server binds only to `127.0.0.1` and serves the report, not the project directory. Reopening a report refreshes its layout with the current Blindspot version without rerunning tests.

Each run lives in `PROJECT/.blindspot/runs/RUN_ID/`:

| File or directory | Contents |
| --- | --- |
| `run.json` | Structured results, source snapshot, and line locations. |
| `report.html` | Browser report. |
| `snapshot/` | Project copy used for the run and replay. |
| `generated/` | Gambit configuration, metadata, and mutant artifacts. |
| `logs/` | Baseline, generation, build, and test logs. |
| `replay-logs/` | Logs from subsequent replays. |

Ctrl+C stops active processes and preserves completed results without opening the browser, even with `--open`. Use `blindspot report` to open those results later. Pending mutations remain marked as not run. Individual mutants can be replayed when their artifacts were retained, but interrupted batches cannot yet be resumed. Worker copies are cleaned up; snapshot retention follows your choice for the run.

## Browse and clean saved runs

```sh
blindspot reports                        # Browse with ↑/↓ and Enter
blindspot reports --list                 # Print report paths
blindspot reports --scan /path/to/projects
blindspot clean                          # Review sizes, select runs, confirm deletion
blindspot clean --project /path/to/project --dry-run
blindspot clean --project /path/to/project --older-than 30d --yes
blindspot clean --all --yes              # Delete all discovered inactive runs and reports
```

The report browser lists projects, dates, run status, and surviving mutation counts, newest first. Selecting a report opens its saved HTML file. It also works when the project no longer has its original `foundry.toml`.

The cleanup menu shows run locations and approximate file sizes. Choose whether to remove replay files while preserving reports, or delete entire runs. Use Space to select runs, A to select all, Enter to continue, then confirm. Press Esc to cancel. Terminals without cursor controls use numbered prompts instead.

Without `--all`, cleanup removes snapshots, generated mutations, build artifacts, and full logs; `run.json` and `report.html` remain. `--all` deletes the selected run directories, including reports. `--yes` applies the displayed plan without a prompt; otherwise noninteractive commands only preview. `--dry-run` never deletes. `--run ID` restricts cleanup to one run and requires `--project`. `--older-than` accepts durations such as `30d` and `24h`.

Cleanup never removes project source files, `.blindspot/settings.json`, the project index, or installed tools. Runs and replays lock their project; cleanup skips busy projects and checks again before deleting. Legacy runs still marked `running` are retained because their activity cannot be verified. Deleting the latest run updates the latest-report pointer. Cleaned reports remain readable but no longer offer replay.

New runs register their project path in a private local index: `~/Library/Application Support/blindspot/projects` on macOS, or `$XDG_CONFIG_HOME/blindspot/projects` (usually `~/.config/blindspot/projects`) on Linux. Set `BLINDSPOT_STATE_DIR` to relocate this index. No index data is uploaded.

By default, discovery includes indexed projects and scans your home folder, plus the current directory when outside your home. It skips hidden folders, `node_modules`, `Library`, `Applications`, and symlinks, and reports inaccessible locations. Use `--scan PATH` to search an external drive or a skipped folder explicitly; repeat it for multiple roots. Explicit `--scan` roots replace the default search, while `--project PATH` selects exactly one project. This avoids traversing the entire filesystem and requires no Forge installation just to browse or clean saved files.

## Replay a mutation

```sh
blindspot replay MUTATION_ID --project /path/to/project --run RUN_ID
```

Omit `--run` to use the latest run. Replay rechecks the original baseline, then evaluates the mutation using the saved source/tests, build and test commands, profile, seed, tool paths, and FFI permission. It does not reinstall dependencies or evaluate your current working files.

The saved tools must remain available. Environment variables and RPC responses are not frozen. Use `--timeout SECONDS` or `--build-timeout SECONDS` to override the recorded limits, or start a new run to evaluate changed source or tests.

## CI

```sh
blindspot run --project /path/to/project --all --fail-on-survivors --json > result.json
```

| Exit code | Meaning |
| --- | --- |
| 0 | Evaluation completed; survivors are informational by default. |
| 1 | Setup, configuration, baseline, or generation failure. |
| 2 | Survivors found with `--fail-on-survivors`. |
| 3 | Incomplete evaluation: timeouts, runner errors, or unfinished work. |
| 130 | Interrupted. |

## Compatibility and isolation

- **Tools:** Gambit v1.0.6, Foundry, and one compatible native Solidity compiler per run. Blindspot detects configured compilers, Foundry's SVM installations, and matching local tools. Use `--solc` if discovery is ambiguous. Mixed-compiler projects need separate compatible configurations.
- **Platforms:** Gambit setup supports macOS and Linux x86-64. Linux ARM requires building Gambit manually and passing `--gambit PATH`. Use WSL on Windows. Blindspot has been run on macOS ARM64 and cross-compiled for Linux; Linux runtime behavior has not been verified here.
- **Project boundaries:** Keep source, dependencies, output paths, and configured filesystem access inside the project root. Internal symlinks are copied; external symlinks are rejected. Use a self-contained Foundry root in monorepos.
- **FFI:** Profiles with `ffi = true` require `--allow-ffi`. It permits the existing setting without changing `foundry.toml`. Project copies are not security sandboxes: subprocesses can affect files and services outside the copy. Helpers must tolerate repeated runs; use `--jobs 1` if they share external state. Replay retains the original FFI permission.
- **Reproducibility:** Pin fork blocks and use stable tests. Configured RPC URLs require a block number; tests creating forks themselves must pin their own blocks. External services and filesystem side effects can still change results.
- **Privacy:** Add `.blindspot/` to your project's `.gitignore`. Snapshots and logs may contain secrets or local configuration. HTML exports include source and absolute local paths. Review artifacts before sharing them or committing them.
