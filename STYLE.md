# luaugo coding style

This document binds every contributor (human or agent) working on luaugo.

## License header

Every Go source file MUST start with this header:

```go
// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.
```

## Package layout

- `internal/common` &mdash; shared constants only (opcodes, bytecode version
  numbers, capability flags). No business logic. No exported functions that
  require state.
- `pkg/ast` &mdash; pure parsing. No bytecode dependency.
- `pkg/bytecode` &mdash; pure (de)serialization. Depends only on
  `internal/common`. No AST dependency, no VM dependency. This package is
  the **single source of truth** for bytecode binary layout: the compiler
  and the VM loader both call into it.
- `pkg/compiler` &mdash; depends on `pkg/ast` and `pkg/bytecode` only.
- `pkg/vm` &mdash; depends on `pkg/bytecode`. May expose Go-level callbacks
  to `pkg/vm/lib`, but `pkg/vm` itself does not import any `lib/*`.
- `pkg/vm/lib/*` &mdash; one Go file per Lua standard library. Each library
  exports a single `Open(*vm.State)` function plus helpers.
- `cmd/luau` &mdash; CLI only. Wires packages together.

Do **not** introduce cycles. Do **not** import any third-party module
outside the standard library. Do **not** use cgo.

## Contract files are append-only

These files define the public surface seen by other tiers and may not be
modified once Tier 1 lands without escalation to the orchestrator:

- `internal/common/*.go`
- `pkg/ast/contract.go`
- `pkg/bytecode/contract.go`
- `pkg/compiler/contract.go`
- `pkg/vm/contract.go`
- `pkg/vm/lib/contract.go`

You may add new exported symbols, but you may not rename or remove any.

## Naming conventions

- Go-idiomatic naming everywhere: `CamelCase` for exported, `camelCase`
  for internal. We do not preserve C `lua_` / `luaL_` prefixes in package
  names; instead the Lua C API lives in `pkg/vm` as methods on `*State`
  (for example `state.PushNumber(x)` mirrors `lua_pushnumber`).
- Opcodes use the upstream `LOP_*` spelling translated to Go: `OpLoadNil`,
  `OpLoadN`, `OpGetImport`, etc. The numeric values **must** match
  upstream exactly.
- Type tags use `TNil`, `TBoolean`, `TNumber`, `TString`, `TTable`,
  `TFunction`, `TUserdata`, `TThread`, `TBuffer`, `TVector`.
- Errors: prefer wrapped sentinels via `fmt.Errorf("...: %w", err)`. VM
  runtime errors are values implementing the `vm.Error` interface so they
  can flow through `pcall` without going through Go's `panic`/`recover`.

## Bytecode discipline

- All integer fields on disk are little-endian.
- All variable-length sizes use Luau VARINT encoding (7 bits per byte,
  continuation bit in the MSB).
- All floating-point constants are IEEE 754 doubles encoded as 8
  little-endian bytes.
- Strings are stored once in a per-blob string table; references are
  1-based VARINTs (0 means "no string").

## GC discipline

- Every value that the user can observe via the Lua API must be a
  garbage-collected object owned by a `*vm.State`. Do not rely on the Go
  GC for Lua-visible lifetimes.
- Write barriers are explicit. Any mutation of a GC object's reference
  field must go through `state.barrier(...)` so the incremental collector
  stays sound.
- The collector is incremental mark-and-sweep, mirroring `lgc.cpp`. Do
  not add a generational hypothesis, do not add concurrent sweeping.

## Coroutines

- One Lua coroutine maps to one Go goroutine. A per-state mutex
  (`State.runMu`) enforces that only one Lua coroutine inside a given
  global state runs at a time, so the VM remains effectively
  single-threaded as Lua expects.
- Yield and resume are implemented via channels held in the coroutine's
  `Thread` struct.

## Testing

- Every package has `_test.go` files in the same directory.
- Conformance tests live under `tests/conformance` and are exercised by
  `go test ./tests/conformance/...`.
- Tests that require the upstream `luau` binary must be guarded by a
  `requireUpstream(t)` helper that skips when the binary is absent.

## Commits

- One commit per tier, signed off by the orchestrator. Worker agents do
  not commit directly; they hand patches to the orchestrator who
  validates and commits the assembled tier.
