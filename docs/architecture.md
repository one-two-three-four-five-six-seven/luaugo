# Architecture

This document describes how luaugo is organized. It is aimed at
contributors and at anyone reading the source.

## Layered structure

```
                  +-----------+
                  |  cmd/luau |   (REPL + script runner)
                  +-----+-----+
                        |
       +----------------v----------------+
       |          pkg/compiler           |   AST -> bytecode.Module
       +----------------+----------------+
                        |
       +----------------v----------------+        +----------------+
       |           pkg/bytecode          | <----> |  pkg/ast       |
       |  (encoder, decoder, IR types)   |        |  (lexer +      |
       +----------------+----------------+        |   parser)      |
                        |                          +----------------+
       +----------------v----------------+
       |             pkg/vm              |   loader, GC, interpreter
       |  +---------------------------+  |
       |  |       pkg/vm/lib          |  |   base, math, string, ...
       |  +---------------------------+  |
       +---------------------------------+
                        |
       +----------------v----------------+
       |       internal/common           |   shared constants
       |  (opcodes, bytecode tags,       |   (no logic)
       |   builtin IDs, capture types)   |
       +---------------------------------+
```

Lower layers do not import higher layers. The `vm` package does not
depend on `ast` or `compiler`; the `compiler` does not depend on `vm`.
This is what allows you to ship a binary that only includes the parts
you need.

## Package reference

### `internal/common`

Shared constants, with no business logic and no exported function that
requires runtime state. Everything here mirrors a value baked into the
bytecode format and therefore must match upstream Luau byte-for-byte:

- `opcodes.go` &mdash; all 86 `Op*` constants plus the instruction-word
  encoding helpers (`EncodeABC`, `EncodeAD`, `EncodeE`, `InsnOp`,
  `InsnA`, `InsnB`, `InsnC`, `InsnD`, `InsnE`, plus the AUX accessors).
- `bytecode.go` &mdash; bytecode version range (`BytecodeVersionMin = 3`,
  `BytecodeVersionMax = 9`, `BytecodeVersionTarget = 9`), type-info
  version range, all 10 `ConstantTag` values, `TypeTag` enumeration,
  `CaptureKind`, `ProtoFlag`.
- `builtins.go` &mdash; the `Builtin` enumeration: every
  `LuauBuiltinFunction` from upstream's `Bytecode.h`.
- `limits.go` &mdash; encoding limits (max register, max upvalue, etc.).

This package never panics, never allocates, has no I/O.

### `pkg/ast`

Lexer and parser. Produces an in-memory tree of `Node` values that
mirror upstream's `AstExpr*` / `AstStat*` / `AstType*` hierarchy.

Key files:

- `contract.go` &mdash; locked interface surface: `Lexer`, `Parse`,
  `ParseExpr`, `Walk`, `PrettyPrint`, plus all `TokenKind` values and
  the `Position` / `Location` / `ParseOptions` / `ParseError` /
  `ParseResult` / `Visitor` types.
- `nodes.go` &mdash; the concrete node types (`ExprConstantNumber`,
  `StatLocal`, `TypeReference`, ~50 total).
- `lexer.go` &mdash; full port of upstream `Lexer.cpp`. Handles every
  Luau literal form, escape sequence, long bracket level, and
  interpolated string.
- `parser.go` &mdash; recursive-descent port of upstream `Parser.cpp`,
  including the upstream `binaryPriority` precedence table.
- `visitor.go` &mdash; `Walk(v, n)`.
- `prettyprint.go` &mdash; debug printer (s-expression format).

The parser is fully decoupled from the bytecode and VM.

### `pkg/bytecode`

Single source of truth for the Luau bytecode binary format. The compiler
and the VM loader both call into this package; neither reimplements the
binary layout.

- `contract.go` &mdash; `Module`, `Proto`, `LineInfo`, `DebugInfo`, the
  10 `Constant*` types, `Decode`, `Encode`, `Disassemble`,
  `VarintAppend`, `VarintRead`.
- `decoder.go` &mdash; loads bytecode bytes into a `*Module`. Mirrors
  upstream `lvmload.cpp`'s `luau_load` byte-for-byte.
- `encoder.go` &mdash; serializes a `*Module` back to bytes. Mirrors
  upstream `BytecodeBuilder.cpp`'s `finalize`. Round-trips identity on
  every real-world Luau blob we have tested.
- `varint.go` &mdash; both 32-bit and 64-bit LEB128-style varints.
- `disasm.go` &mdash; informational text dump.

### `pkg/compiler`

AST &rarr; `bytecode.Module`.

- `contract.go` &mdash; `Compile`, `CompileSource`, `CompileBinary`,
  `Options`, optimization/debug/coverage/type-info level enums,
  `Defaults`, `CompileError`.
- `compiler.go` &mdash; the main walker (~3 kLOC). One pass with
  on-the-fly value tracking, scope management, and emission.
- `builder.go` &mdash; in-memory IR helpers (`protoBuilder`) wrapping
  `bytecode.Proto` construction with register/jump/constant bookkeeping.
- `constants.go`, `costmodel.go`, `tableshape.go`, `valuetracking.go`,
  `builtinfolding.go`, `builtins.go`, `typeinfo.go` &mdash; ported
  helpers from upstream `Compiler/src/*.cpp`.

The compiler does not depend on the VM. Its tests verify (a) clean
compilation, (b) end-to-end execution on the upstream VM through the
`bcrunner` differential harness.

### `pkg/vm`

The Luau virtual machine. The largest package.

Object model:

- `object.go` &mdash; `value` (a tagged union of nil / bool / number /
  vector / string / table / closure / userdata / thread / buffer).
- `string.go` &mdash; `tString` plus the per-state intern table.
- `table.go` &mdash; hybrid array+hash table with frozen flag, `next`
  iteration matching upstream's `luaH_next`.
- `closure.go` &mdash; `closure` (Lua or Go variant) and `upVal` (open
  and closed states with write barrier on close).
- `userdata.go` &mdash; `userdata` with tag, metatable, and finalizer
  hook.
- `buffer.go` &mdash; fixed-size byte array with read/write helpers.
- `vector.go` &mdash; `Vector` type with 3-wide default.

Garbage collector:

- `gc.go` &mdash; tri-color incremental mark-and-sweep with write
  barriers, weak-table clearing, userdata `__gc` finalization.
- `mem.go` &mdash; per-object size accounting (drives `GCInfo`).

Interpreter:

- `state.go` &mdash; `stateImpl` (the unexported impl behind `*State`)
  and `globalState` (shared by coroutines).
- `execute.go` &mdash; the main `for { switch }` dispatch loop. One
  case per `common.Op*`. Mirrors upstream `lvmexecute.cpp`.
- `do.go` &mdash; call frames, `callValue`, `pcallFromGo`, upvalue
  closing.
- `load.go` &mdash; bytecode-module loader: links protos, allocates
  closures, pushes the main function.
- `arith.go`, `compare.go` &mdash; arithmetic, equality, ordering with
  metamethod dispatch.
- `tm.go` &mdash; tag-method (metamethod) cache and dispatch.
- `thread.go` &mdash; coroutine scheduler: one goroutine per coroutine,
  one mutex per global state.
- `debug.go` &mdash; `TraceBack`, `GetInfo` for `debug.traceback` /
  `debug.info`.
- `builtins.go` &mdash; FASTCALL builtin dispatch table; 25 fast paths
  for math, bit32, string, raw ops, table, meta/type ops.
- `api.go`, `auxlib.go` &mdash; the Lua C API methods exposed on
  `*State`.
- `contract.go` &mdash; the public surface: `Type`, `Status`,
  `*State` and all its methods, `GoFunction`, `Error` interface.
- `bufferapi.go`, `tablex.go` &mdash; small additional exported helpers.

### `pkg/vm/lib`

Standard libraries. One Go file per Lua library:

- `base.go` &mdash; global functions (`print`, `assert`, `pcall`, ...).
- `math.go` &mdash; `math` table (incl. 3D Perlin noise).
- `string.go` &mdash; `string` table (incl. complete pattern matcher,
  `format`, `pack`/`unpack`/`packsize`, the string metatable wiring).
- `table.go` &mdash; `table` table.
- `coroutine.go` &mdash; `coroutine` table.
- `bit32.go` &mdash; `bit32` table.
- `utf8.go` &mdash; `utf8` table.
- `os.go` &mdash; `os` table.
- `debug.go` &mdash; `debug` table.
- `buffer.go` &mdash; `buffer` table.
- `vector.go` &mdash; `vector` table.

Each file exports a single `Open<Name>(s *vm.State)` function that
registers its globals or library table. `OpenAll(s)` opens them all.

### `cmd/luau`

Command-line front-end. Reads a script, invokes the compiler, runs the
result on the VM with the standard library opened.

### `internal/upstreamvm`

Test-only helper. Drives the optional `bcrunner` harness (built from
`tools/luau-bcrunner`) that links statically against the upstream Luau
VM. Used by the conformance test to verify that luaugo-compiled
bytecode actually runs on the real Luau VM.

### `tools/luau-bcrunner`

Small C++ harness (~120 lines) that links against the upstream Luau VM
source under `.upstream/VM/src/*.cpp` and exposes a CLI that loads a
`.luac` blob and executes it. Used only by tests; the binary is
gitignored, so contributors who don't run the differential tests do
not need a C++ toolchain.

### `tools/demo`

A standalone end-to-end demonstration test: compile a 40-line Luau
program with the luaugo compiler, execute it on the upstream VM,
assert expected output.

### `tests`

Top-level conformance test directory.

- `conformance/` &mdash; the 53 `.luau` fixtures mirrored from upstream
  Luau's test suite.
- `golden/` &mdash; reference `.luac` blobs produced by upstream
  `luau-compile --binary` for each fixture; used by
  `pkg/bytecode/roundtrip_test.go` to verify byte-level decoder
  fidelity on real bytecode.
- `conformance_suite_test.go` &mdash; the headline integration test:
  for every fixture, compile with both upstream and luaugo, run both
  blobs on the upstream Luau VM, report side-by-side.
- `fixtures_test.go` &mdash; sanity check that the conformance corpus
  is intact (53 files present).

## Data flow

### Compiling a script (top to bottom)

1. `compiler.CompileBinary("foo.luau", source, Defaults())` is called.
2. The compiler calls `ast.Parse(name, source, ParseOptions{})`. The
   lexer tokenizes; the parser builds an `*ast.Program`.
3. The compiler walks the program in `compiler.go`. Locals, upvalues,
   constants, and child protos are tracked in `protoBuilder`s.
4. Each function emits a `*bytecode.Proto`.
5. The compiler returns a `*bytecode.Module`.
6. `bytecode.Encode(module, EncodeOptions{})` serializes the module
   into a `[]byte` whose layout is byte-faithful to upstream Luau's
   `BytecodeBuilder::finalize`.

### Running bytecode (top to bottom)

1. `state.Load("foo.luau", blob, 0)` is called.
2. `vm/load.go` calls `bytecode.Decode(blob)` to get the
   `*bytecode.Module`.
3. For each proto in the module, the loader allocates a runtime `Proto`
   in the GC heap, copies the code, resolves import references, and
   converts the `Constant*` entries into runtime values.
4. The main proto is wrapped in an `LClosure` and pushed onto the
   state's stack.
5. `state.PCall(...)` enters the call dispatcher in `vm/do.go`, which
   invokes `executeProto` from `vm/execute.go`.
6. `executeProto` runs a `for { switch op {...} }` loop, advancing the
   program counter and reading from the stack. Metamethod-bearing
   operations (`OpAdd`, `OpGetTable`, etc.) call into `arith.go`,
   `compare.go`, and `tm.go`.
7. On `OpReturn`, the frame is popped and `executeProto` returns to
   the caller frame (or to `PCall` if the main chunk).

### A coroutine

1. `s.NewThread()` allocates a `coroutine` struct with two channels
   (`resumeCh` and `yieldCh`) and a fresh `stateImpl` sharing the
   parent's `globals` and `gs`.
2. `co.Resume(parent, nargs)`:
   - Spawns the coroutine goroutine if it is the first resume.
   - Acquires `gs.vmMutex` (per-global-state mutex enforcing
     single-coroutine-at-a-time execution).
   - Sends the args on `resumeCh`.
   - Waits on `yieldCh` for the next yield, return, or error.
3. Inside the coroutine, `co.Yield(nresults)`:
   - Sends a `yieldMsg` on `yieldCh`.
   - Drops `gs.vmMutex`.
   - Waits on `resumeCh` for the next resume.
4. The main thread runs synchronously on the caller's goroutine; only
   non-main coroutines have their own goroutines.

## Dependencies

luaugo depends only on the Go standard library. No third-party Go
modules. No cgo. The repository has no `go.sum` because there is
nothing to vendor.

## Source-of-truth references

- Opcodes, constants, version constants: `.upstream/Common/include/Luau/Bytecode.h`
- Bytecode binary layout (writer): `.upstream/Bytecode/src/BytecodeBuilder.cpp`
- Bytecode binary layout (reader): `.upstream/VM/src/lvmload.cpp`
- Interpreter loop: `.upstream/VM/src/lvmexecute.cpp`
- Object model + GC: `.upstream/VM/src/lobject.h`, `lgc.cpp`, `ltable.cpp`, `lstring.cpp`
- Compiler: `.upstream/Compiler/src/Compiler.cpp`
- Each standard library: `.upstream/VM/src/l<name>lib.cpp`

The full upstream-to-Go mapping is documented in `tools/UPSTREAM_MAP.md`.
