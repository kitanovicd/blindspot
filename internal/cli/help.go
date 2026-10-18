package cli

const help = `
  blindspot — mutation coverage for Foundry

  Run your tests against Gambit mutations. See the gaps at the source.

  Commands
    (no command)               Start guided setup in a terminal
    run [files/directories]    Evaluate mutations and save a browser report
    report                     Open the latest report (no server required)
    replay <mutation-id>       Re-test one mutation from the saved snapshot
    doctor                     Check Foundry, Gambit, and compiler setup
    setup                      Download and verify Gambit in your user cache
    version                    Print the version

  Examples
    blindspot
    blindspot run src/Vault.sol --limit 30 --open
    blindspot run src/Vault.sol --line 16 --all
    blindspot run --changed origin/main --jobs 2
    blindspot report --serve
    blindspot replay m-0123456789ab

  Run options
    --interactive              Ask for the project and setup commands
    --install-command CMD      Install dependencies once in the original project
    --build-command CMD        Compile during setup, baseline, and mutations
    --test-command CMD         Run tests during baseline and mutations
    --project PATH             Foundry project directory (default: current)
    --limit N                  Maximum mutants to evaluate (default: 50)
    --all                      Evaluate all generated mutations in scope
    --line N                   Mutate one original line in the selected file
    --jobs N                   Parallel workers (default: 2, bounded by CPUs)
    --timeout SECONDS          Test execution limit (default: 120)
    --build-timeout SECONDS    Compilation limit (default: max of 600 and --timeout)
    --seed N                   Recorded mutation and fuzz seed (default: 42)
    --profile NAME             Foundry profile (default: FOUNDRY_PROFILE or default)
    --allow-ffi                 Permit FFI subprocesses when the profile enables them
    --solc PATH                Native solc executable; otherwise auto-resolved
    --gambit PATH              Gambit executable; otherwise auto-resolved
    --changed REF              Mutate source files changed since this Git ref
    --open                     Open the completed HTML report
    --fail-on-survivors         Exit 2 if any mutation survives (for CI)
    --json                     Write final results as JSON to stdout

  Exit codes: 0 complete · 1 setup/baseline failure · 2 survivors (opt-in)
              3 incomplete evaluation · 130 interrupted

  Reports stay in PROJECT/.blindspot/. Mutations run in separate project copies.
  Full docs: https://github.com/kitanovicd/blindspot#readme
`
