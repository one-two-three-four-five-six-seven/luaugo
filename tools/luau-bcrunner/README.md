# luau-bcrunner

A small C++ harness that loads a precompiled Luau bytecode blob into
the **official upstream Luau VM** and executes it. luaugo's compiler
tests pipe the bytes they emit through this harness to verify that
our bytecode is semantically valid Luau bytecode (i.e. it loads
without error and runs to completion against the real VM).

This is the primary correctness gate for the luaugo compiler:
**bytecode produced by luaugo must run on the real Luau VM.**

## Building

```
powershell -File tools/luau-bcrunner/build.ps1
```

Requires:
- g++ on PATH (or the workspace's mingw64 at
  `C:\Users\user\Documents\mingw64\bin\g++.exe`)
- Upstream Luau source checked out at `.upstream/` (done by
  `tools/sync-upstream.ps1`)

Produces `tools/luau-bcrunner/bcrunner.exe`. The binary is
gitignored; rebuild it whenever you re-sync upstream.

## Usage

```
bcrunner.exe path/to/blob.luac
```

Exit codes:
- 0 -- bytecode loaded and main chunk returned (return values printed
  to stdout, one per line)
- 1 -- I/O error reading the bytecode blob
- 2 -- `luau_load` rejected the bytecode (load-time error on stderr)
- 3 -- `lua_pcall` returned a runtime error (error on stderr)

The harness opens `luaL_openlibs` and applies `luaL_sandbox` /
`luaL_sandboxthread` before loading, matching upstream's default REPL
configuration.
