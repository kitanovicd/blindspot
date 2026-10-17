# Third-party tools

Blindspot uses separate development/runtime executables from the projects below. Local copies may be installed in `.tools/`, which is excluded from source control; their binaries and local license copies are not part of this source repository.

- Gambit v1.0.6: https://github.com/Certora/gambit — MIT. Blindspot invokes the standalone executable.
- Solidity: https://github.com/argotorg/solidity — GNU GPL v3. Blindspot invokes a native compiler compatible with the tested project. The demo uses v0.8.28; corresponding source: https://github.com/argotorg/solidity/tree/v0.8.28.
- Go toolchain and standard library: https://go.dev — BSD-style license, available in the Go distribution's `LICENSE` file.

The Blindspot executable uses only the Go standard library. Gambit and Solidity are separate programs, not linked into Blindspot. Do not redistribute a binary bundle without preserving its applicable notices and satisfying upstream license obligations.
