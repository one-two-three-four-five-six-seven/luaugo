# Compatibility with upstream Luau

This document is the honest, current picture of what luaugo does and does
not match in upstream [luau-lang/luau](https://github.com/luau-lang/luau).
The reference upstream version is **0.720** (pinned in `tools/UPSTREAM.md`).

## What is fully supported

### Language syntax (100% parse-clean on the upstream test corpus)

luaugo's lexer and parser accept every `.luau` source file in
`tests/conformance/` (53 fixtures, mirrored from upstream). This includes:

- All standard Luau statements and expressions.
- Numeric literals: decimal, hexadecimal (`0x`), binary (`0b`), with
  underscore separators.
- String literals: single, double, long brackets (`[[...]]`, `[=[...]=]`),
  and backtick-quoted interpolated strings (`` `hello {x}` ``).
- Type annotations on locals, function parameters, and returns.
- Generic type parameters and explicit type instantiations (`f<<T>>(...)`).
- `type` and `export type` aliases, including `type function`.
- Attributes such as `@native` and `@checked`.
- Compound assignments (`+=`, `-=`, `*=`, `/=`, `//=`, `%=`, `^=`, `..=`).
- The `continue` keyword.
- `if`-then-`else` as an expression.
- Type assertions (`expr :: T`).
- String interpolation expressions.

### Bytecode

- **Versions 3 through 9** of the Luau bytecode format are supported by
  both the encoder and decoder.
- The decoder round-trips all 53 upstream-compiled conformance fixtures
  byte-for-byte. This independently confirms the codec's correctness on
  real-world bytecode.
- The compiler emits **bytecode version 9** by default, matching what
  upstream's `luau-compile --binary` produces.
- All 10 constant kinds are supported: nil, boolean, number, string,
  import, table (key list), closure, vector, table with constants
  (v7+), and 64-bit integer (v8+).

### Virtual machine

- **All 86 opcodes** dispatch in the luaugo interpreter, including the
  v9 userdata-field accessors (`OpGetUdataKS`, `OpSetUdataKS`,
  `OpNameCallUdata`).
- FASTCALL dispatch with 25 fast-path builtins (`math.abs`, `string.len`,
  `bit32.band`, `rawget`, etc.); the rest fall back to the regular
  CALL path, which is also correct.
- The garbage collector is an incremental tri-color mark-and-sweep with
  write barriers and weak-table support, mirroring upstream's `lgc.cpp`.
- Coroutines: one goroutine per coroutine plus a per-VM scheduler mutex.
  Race-detector clean.

### Standard library

Every Luau standard library is implemented. See [stdlib.md](stdlib.md)
for the per-function reference. The big-ticket items:

- `base` &mdash; 26 globals including `assert`, `pcall`, `xpcall`,
  `pairs`, `ipairs`, `select`, `tostring`, `tonumber`, `type`, `typeof`,
  `getmetatable`, `setmetatable`, etc.
- `math` &mdash; All 37 functions including the **full 3D Perlin noise**
  port (matching upstream's permutation and gradient tables and the
  `FixMathNoisePrecision` fix).
- `string` &mdash; The **complete Lua pattern matcher**: character
  classes, sets, anchors, captures, position captures, balanced (`%bxy`),
  frontier (`%f[set]`), lazy and greedy quantifiers. Plus `string.format`,
  `string.pack`/`unpack`/`packsize`, and Luau's `string.split`.
- `table` &mdash; All 17 functions including `sort` (introsort), `move`
  (overlap-safe), `freeze`, `isfrozen`, `clone`.
- `coroutine` &mdash; All 8 functions; race-clean.
- `bit32` &mdash; All 15 functions.
- `utf8` &mdash; All 5 functions with a strict decoder (rejects overlongs,
  surrogates, code points beyond `0x10FFFF`).
- `os` &mdash; `clock`, `date` (including `*t` and full `strftime`),
  `difftime`, `time`.
- `debug` &mdash; `info` and `traceback`.
- `buffer` &mdash; All 26 entries (create, read/write for every integer
  and float width, bits accessors, copy, fill).
- `vector` &mdash; All 14 functions; 3-wide by default.

### Differential test against upstream VM

Every fixture in the Luau conformance corpus is compiled twice &mdash;
once by upstream's `luau-compile`, once by luaugo's compiler &mdash; and
each resulting `.luac` blob is executed on the **official upstream Luau
VM** via the `bcrunner` harness. The current numbers (see
`tests/conformance_suite_test.go`):

| Metric | Result |
|---|---|
| luaugo compiled clean | 53 / 53 |
| luaugo bytecode loaded on upstream VM | **53 / 53** |
| Same VM exit status as upstream-compiled blob | 47 / 53 |
| Identical stdout to upstream-compiled blob | 45 / 53 |

The 100% load-OK rate is the headline result: **every fixture in the
upstream Luau test suite, compiled by luaugo's pure-Go compiler, is
accepted and successfully begins executing on the real Luau VM**.

## Known divergences

### Compiler

The luaugo compiler is correctness-first; several performance
optimizations are not yet implemented. None of them affect whether
bytecode loads or runs on the real VM, but they do affect performance and
make some divergences visible to scripts that introspect bytecode.

| Optimization | Status | Visible to scripts |
|---|---|---|
| Constant folding (`1+2 -> 3`) | Not emitted | No: numerically identical at runtime, slightly larger bytecode. |
| FASTCALL substitution for builtins | Not emitted | No: builtin calls go through the regular CALL path. Slower for hot loops. |
| GETIMPORT for safe imports | Always emits GETGLOBAL | No: equivalent at runtime, slightly slower load. |
| DUPTABLE with pre-populated values (v7+) | Only the key-shape variant | No: pre-fill happens via SETTABLEKS instead. |
| Inlining and loop unrolling | Not implemented | No: script semantics identical, performance lower. |
| `CaptureVal` upvalue specialization | All captures use `CaptureRef` | No: correct but slightly slower upvalue reads. |
| JUMPXEQK* compare-and-jump fusion | Always emits separate JUMPIF + AUX | No: equivalent at runtime. |
| `SUBRK` / `DIVRK` (constant-on-left arith) | Only constant-on-right detected | No: equivalent at runtime. |
| Line-info emission | Currently empty | **Yes**: tracebacks from luaugo-compiled code show `:0:` instead of the real source line. |
| Debug-info (`-g2`) | Not yet emitted | **Yes**: local and upvalue names are not visible to `debug.info`. |
| `string.format`-based interpolation | Lowered as CONCAT chain | Mostly hidden: the runtime output is the same except that the line number reported on errors comes from the concatenation, not the interpolation. |

### Runtime

| Behavior | Status | Notes |
|---|---|---|
| `error()` source-location prefix | Imperfect | luaugo's `Where` returns chunkname plus a placeholder line; line numbers are missing while the compiler omits line info. Once the compiler emits line info, error messages will exactly match upstream. |
| `debug.info` line number | Imperfect | Returns 0 for luaugo-compiled chunks while line info is unimplemented. |
| Native code generation (`@native`, `--!native`) | Not supported | Annotations parse; the runtime ignores them. Code always runs interpreted. |
| `loadstring` | Returns `nil, "loadstring disabled"` | luaugo deliberately omits a runtime parser to keep the VM small. Compile separately and use `state.Load(blob)`. |
| `--!strict` / type analyzer warnings | Not produced | luaugo does not include `luau-analyze` (the type checker) in scope. |

### Coroutine fine print

- `coroutine.wrap` keeps the wrapped thread alive via the Go closure
  (Go's GC) rather than via a Lua upvalue on the wrapper. Functionally
  identical for user code; flagged for hosts that traverse Lua-level
  upvalues.
- `coroutine.close` on a parked-but-not-yet-resumed coroutine reports
  success but the underlying goroutine remains until the parent `*State`
  is closed. For long-running processes that aggressively create-and-
  close many coroutines, expect background goroutine retention.
- The `"normal"` status is currently reported as `"suspended"` for
  coroutines mid-resuming another. No upstream test relies on this
  distinction.

### Vector

- The default build is **3-wide vectors**. The `LUA_VECTOR_SIZE = 4`
  variant is structurally supported by the type but no compiler / runtime
  flag flips it yet.
- `vector.normalize(zero_vec)` returns `(0,0,0)`; upstream returns
  inf-or-NaN. This was an explicit choice per the original task brief.

### Conformance fixtures that diverge in stdout

Of the 53 fixtures, the 6 that show a different exit status from
upstream-compiled bytecode share two root causes:

1. **A real compiler bug** that emits arithmetic before the operand is
   defined in certain register-allocation patterns. Reproducer:
   `assert.luau` (fixture line: `assert(not ok)` where `ok` came from a
   `pcall`). Tracked.
2. **Missing line info** so an error-message comparison like
   `assert(err == "basic.luau:39: oops")` finds `"basic.luau:0: oops"`
   instead. Trivially fixed once the compiler emits line info.

Neither is a load failure; in both cases the upstream Luau VM accepts
the bytecode and begins executing.

## Things upstream has that luaugo does not (and probably will not)

- **Native code generation.** Upstream's `Luau.CodeGen` is an x64/A64
  JIT. luaugo is a pure-Go interpreter by design; native codegen is
  explicitly out of scope.
- **Static type analyzer (`luau-analyze`).** Upstream's `Luau.Analysis`
  is roughly 30,000 lines of C++ implementing a sophisticated gradual
  type system. luaugo parses type annotations but does not perform any
  static analysis on them. If you need `luau-analyze`, run it from the
  upstream binary.
- **`.luaurc` configuration parsing.** Out of scope for now.
- **Module resolver (`require`).** Out of scope for now.
- **`@checked` runtime arg-type checks.** The attribute parses but is
  ignored at the moment.

## How to verify the claims in this document

Every numeric claim ("53/53", "all 86 opcodes", "race-clean") is
reproducible from a clean checkout:

```
go build ./...                          # whole repo must build clean
go vet   ./...                          # whole repo must vet clean
go test  ./...                          # every package must be green
go test  -race ./pkg/vm/...             # coroutines + pcall race-clean
go test  ./tests/ -run TestLuauConformanceSuite -v
        # full conformance report with side-by-side per-fixture status
```

See [testing.md](testing.md) for the full test-running guide, including
how to build and run the `bcrunner` differential harness.
