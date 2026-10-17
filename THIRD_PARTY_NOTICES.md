# Third-party tools

Blindspot uses separate development/runtime executables from the projects below. Gambit is installed in the user's cache; source checkouts may also use `.tools/`, which is excluded from source control. Their binaries are not part of this source repository or Blindspot release archives.

- Gambit v1.0.6: https://github.com/Certora/gambit — MIT. Blindspot invokes the standalone executable.
- Solidity: https://github.com/argotorg/solidity — GNU GPL v3. Blindspot invokes a native compiler compatible with the tested project.
- Go toolchain and standard library: https://go.dev — BSD-style license, available in the Go distribution's `LICENSE` file.

The Blindspot executable uses only the Go standard library. Release archives include its BSD-style license as `GO_LICENSE`. Gambit and Solidity are separate programs, not linked into Blindspot. Preserve the applicable licenses and notices when redistributing binaries.
