# Frequently asked questions

## Is luaugo a drop-in replacement for upstream Luau?

For the **compiler and VM**, mostly yes:

- Source code that parses on upstream Luau parses on luaugo.
- Bytecode produced by luaugo loads on the official Luau VM.
- Scripts running on the luaugo VM see the same standard library.

For the **type analyzer** (`luau-analyze`), no &mdash; luaugo does not
yet include one. Type annotations are parsed and ignored at compile
time, which is fine for runtime correctness but means you won't get
static type-check warnings.

For the **native code generator** (the upstream JIT), no &mdash; luaugo
is a pure-Go interpreter by design. `@native` annotations parse and are
ignored. Code always runs interpreted.

See [compatibility.md](compatibility.md) for the precise behavioral
matrix.

## Why a pure-Go port and not a cgo binding?

Three reasons:

1. **Portability**: a pure-Go module cross-compiles trivially to every
   platform Go supports, with no native toolchain required at the
   consumer's build site.
2. **Distribution**: no DLL/dylib/so management, no ABI compatibility
   issues, no platform-specific linker flags.
3. **Tooling**: `go test`, `go vet`, `go build -race`, and standard Go
   profiling all work without special configuration.

The cost is performance: a Go interpreter will not match the
JIT-accelerated upstream VM on hot loops. For embedders where the
overhead is dominated by host calls (the typical case for games and
scripting layers), the difference is acceptable.

## Can I run bytecode from Roblox?

If the bytecode is in the format upstream Luau produces (versions 3
through 9), yes. Pass the bytes to `state.Load(name, blob, 0)` and run
it.

Be aware that Roblox-specific globals (workspace, Instance, etc.) are
**not** registered by `lib.OpenAll`. You must wire those up yourself
via `state.PushGoFunction` and the rest of the Go-side API.

## Can I compile to bytecode and ship a precompiled `.luac`?

Yes. Compile with:

```
luau --compile=binary script.luau > script.luac
```

(or use `compiler.CompileBinary` from Go). The resulting blob runs on
both the luaugo VM and the official Luau VM.

## Does luaugo support `--!native`?

The `--!native` and `@native` annotations are parsed and accepted, but
luaugo does not have a native code generator. They have no effect at
runtime.

## What's the practical relationship between byte-identity and
correctness?

luaugo's bytecode is **not** byte-identical to upstream's bytecode for
the same source, by design. The luaugo compiler does not implement
every optimization upstream does (constant folding, FASTCALL
substitution, GETIMPORT specialization, etc.), so it emits a different,
slower, but semantically equivalent instruction sequence.

The contract that **is** maintained is that luaugo-emitted bytecode
**loads and runs on the upstream Luau VM**. All 53 upstream
conformance fixtures pass this gate.

## Why "Luau" with a lowercase `u` and pronounced "loo-au"?

That's upstream's choice. luaugo respects it: the language is `luau`,
the project name is `luaugo`.

## How is the GC tuned?

luaugo runs an incremental tri-color mark-and-sweep collector mirroring
upstream's `lgc.cpp` semantics. It runs at safepoints: function return,
backward jumps (loop iteration), table construction, and closure
creation. There is no concurrent or generational behavior; the
collector is single-threaded with the executor.

Concretely, you don't need to call `CollectGarbage` in normal programs.
Call it before a memory-sensitive measurement or in a long-running
host that wants to reclaim space at a known moment.

## Are coroutines goroutines?

Yes, one Go goroutine per Lua coroutine. A per-VM mutex ensures that
only one of a global state's coroutines is executing at a time, so the
visible semantics match Lua exactly (cooperative, deterministic).

The race detector is clean for coroutine and pcall paths.

## Can I run multiple VMs concurrently?

Yes. Independent `*vm.State` values share no state with each other.
Create one per goroutine if you want true parallelism.

A single `*vm.State` is **not** safe for concurrent use from multiple
goroutines &mdash; if you need that, wrap it in your own `sync.Mutex`.

## What happens if a Go function panics inside a Luau callback?

If the function is called through `s.Call`, the panic propagates up to
the Go caller of `Call`. If it is called through `s.PCall`, the
runtime catches it, turns it into a Luau error, and returns
`StatusErrRun`. The error value (a Go-native string in this case) is
on top of the stack.

Inside a Go callback, prefer `s.Errorf(...)` over `panic(...)` for
errors you intend to surface to Luau, because `Errorf` wraps cleanly
with `pcall`/`xpcall` and tags the error as a Luau runtime error rather
than a Go panic.

## Where can I find the source for an upstream Luau file?

The repository pin is in `tools/UPSTREAM.md`. Run
`tools/sync-upstream.ps1` to clone the upstream tag into `.upstream/`,
then read directly. The mapping from luaugo packages to upstream files
is in `tools/UPSTREAM_MAP.md`.

## Why do I see CRLF warnings on Windows during `git commit`?

Git is normalizing line endings. It's harmless. If you want to silence
it, set `core.autocrlf=input` in your local git config.

## How do I report a bug?

Open an issue on the GitHub repository with:

1. A minimal `.luau` source file (or, even better, a failing Go test).
2. The expected behavior on the upstream VM (`luau script.luau` output
   or the relevant upstream conformance test).
3. The actual behavior on luaugo.
4. The Go version (`go version`) and OS.

Compiler bugs that affect bytecode validity should mention whether the
blob passes `bcrunner` on the upstream VM &mdash; that quickly tells us
whether the bug is in the compiler or the in-process VM.

## Can luaugo run Lua 5.x code (not Luau)?

Mostly. Luau is largely backwards-compatible with Lua 5.1 but extends
the language. Lua 5.1 source files that don't use Lua 5.2+ syntax
(`goto`, integer division `//` used as such instead of as `string.split`
style usage, etc.) should compile and run unchanged.

Lua-specific stdlib functions that Luau drops (`module`, `package`,
`io`, `dofile`, file-based `loadfile`) are not available; Luau's
sandboxed model deliberately excludes I/O.

## Does the luaugo VM support `setfenv`/`getfenv`?

`getfenv` returns the `_G` proxy table. `setfenv` is a no-op that raises
an error. Upstream Luau has deprecated these and they have minimal
real-world use; full implementation is not planned.

## What is the license?

MIT. luaugo preserves upstream Luau's MIT license and the original Lua
5.x MIT license. See `LICENSE.txt` and `lua_LICENSE.txt`.
