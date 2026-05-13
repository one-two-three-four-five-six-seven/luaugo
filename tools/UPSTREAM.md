# Upstream Luau pinning

luaugo is ported from a specific upstream commit, not "tip of master".
This file is the single source of truth for the pin.

## Current pin

- Repository: https://github.com/luau-lang/luau
- Tag: `0.720`
- Bytecode versions supported: **3 through 9** (LBC_VERSION_MIN=3, LBC_VERSION_MAX=9)
- Bytecode type info versions supported: **1 through 3**

## How to bump

1. Pick the new upstream tag.
2. Update this file with the new tag and bytecode version constants
   visible in `Common/include/Luau/Bytecode.h`.
3. Re-run `tools/sync-upstream.ps1` to refresh `tests/conformance/`
   fixtures and regenerate `tests/golden/*.luac`.
4. Run `go test ./...` and triage any failures.
5. Commit the bump as its own commit (do not mix with code changes).
