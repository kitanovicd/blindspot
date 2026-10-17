# Blindspot

Find gaps in your Foundry tests with [Certora Gambit](https://github.com/Certora/gambit).

Blindspot changes your Solidity code one mutation at a time, runs your existing tests, and shows which changes went undetected in a local browser report. Your original files are never changed. Blindspot does not generate or fix tests.

## Install

Requires **Go 1.23+** and **Foundry** (`forge` on your PATH).

```sh
git clone https://github.com/kitanovicd/blindspot.git
cd blindspot
go build -trimpath -o blindspot .
./blindspot setup
```

`setup` downloads and verifies Gambit v1.0.6. It supports macOS and Linux x86-64; Windows users need WSL. No Node, npm, account, or hosted service is needed.

Build your Foundry project once to install its Solidity compiler, then check the setup:

```sh
forge build --root /path/to/project
./blindspot doctor --project /path/to/project
```

## Run

Start with one file and a small sample:

```sh
./blindspot run --project /path/to/project src/Vault.sol --limit 5 --open
```

For all mutations across the project:

```sh
./blindspot run --project /path/to/project --all --open
```

Blindspot first checks that your original build and tests pass. It then compiles and runs the full configured test suite for each mutation, using separate project copies. Your Foundry profile, test filters, and compiler settings are preserved.

- Without `--limit` or `--all`, it evaluates up to **50 mutants**.
- `--limit` is a maximum. If Gambit finds five mutations, `--limit 20` still evaluates five.
- `--all` removes the mutant limit, but timeouts still apply.
- Paths are relative to the project. Omit `--project` to use the current directory.

Each mutant needs a build and test run. Start small on large projects.

## Understand the report

| Result | Meaning |
| --- | --- |
| Survived | Tests passed despite the change. This may be a test gap. |
| Caught | Tests failed because of the change. |
| Invalid | The mutation did not compile. Excluded from evaluated coverage. |
| Unresolved | A timeout, runner error, or interruption prevented a result. |

The report groups surviving mutations by file and line. Expand a line to inspect the change, view logs, or copy a replay command.

**Mutation coverage is not line execution coverage.** Some surviving changes preserve behavior. Lines without evaluated mutations are unassessed, not proven covered.

## Reopen or replay

```sh
# Open the latest report without running tests
./blindspot report --project /path/to/project

# Recheck one mutation from a saved run
./blindspot replay MUTATION_ID --project /path/to/project --run RUN_ID
```

Runs are saved in `PROJECT/.blindspot/`. Add `.blindspot/` to your project's `.gitignore`: snapshots and logs may contain local configuration or secrets. HTML reports include source and local paths, so review them before sharing.

Replay uses the saved project snapshot. Start a new run to evaluate changed code or tests.

See the **[usage guide](docs/usage.md)** for flags, timeouts, FFI, CI, exporting reports, and compatibility. Use `./blindspot COMMAND --help` for command options.

## Development

Go only, with no third-party Go dependencies.

```sh
make build    # Build ./blindspot
make fmt      # Format Go code
make check    # Run go vet and build
```

```text
main.go               Entry point and signal handling
internal/cli/         Commands, flags, and terminal output
internal/mutation/    Generation, execution, snapshots, and replay
internal/report/      Result model, embedded HTML, and local server
internal/terminal/    Terminal detection and optional colors
internal/toolchain/   Gambit installation and compiler discovery
docs/                 Usage reference
```

Blindspot is independent of Certora, Foundry, and Solidity. See [third-party notices](THIRD_PARTY_NOTICES.md) and [license](LICENSE).
