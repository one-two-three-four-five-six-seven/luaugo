# Upstream Luau pinning

luaugo is ported from a specific upstream commit, not "tip of master".
This file is the single source of truth for the pin.

## Current pin

- Repository: https://github.com/luau-lang/luau
- Tag: `0.720`
- Bytecode versions supported: **3 through 9** (LBC_VERSION_MIN=3, LBC_VERSION_MAX=9)
- Bytecode type info versions supported: **1 through 3**

## Official binaries

The official upstream `luau-compile.exe`, `luau.exe`, `luau-analyze.exe`,
and `luau-ast.exe` for tag `0.720` are vendored under `.upstream-bin/`
(downloaded from
https://github.com/luau-lang/luau/releases/download/0.720/luau-windows.zip).
Test harnesses use `luau-compile.exe --binary <file>` to produce the
reference bytecode the luaugo compiler must match byte-for-byte.

Observed empirical fact: the upstream `luau-compile` binary from tag
0.720 emits **bytecode version 9** by default for every fixture in
`tests/conformance/`. The C++ source defines `LBC_VERSION_TARGET = 6`
but the production binary overrides this; luaugo follows the binary's
actual behavior. `tests/golden/` holds the 53 reference `.luac` blobs.

## How to bump

1. Pick the new upstream tag.
2. Update this file with the new tag and bytecode version constants
   visible in `Common/include/Luau/Bytecode.h`.
3. Re-run `tools/sync-upstream.ps1` to refresh `tests/conformance/`
   fixtures and regenerate `tests/golden/*.luac`.
4. Run `go test ./...` and triage any failures.
5. Commit the bump as its own commit (do not mix with code changes).
