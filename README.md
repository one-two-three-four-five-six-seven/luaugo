# luaugo

A pure-Go port of the [Luau](https://github.com/luau-lang/luau) programming
language, focused on **byte-level bytecode compatibility** with upstream
Luau.

Luau is a fast, small, safe, gradually typed embeddable scripting language
derived from Lua, developed and used by Roblox. luaugo is an independent
reimplementation of the Luau compiler and virtual machine in idiomatic Go,
designed so that:

- **Bytecode produced by the official `luau` compiler runs unchanged on the
  luaugo VM.** Versions v3 through v9 of the Luau bytecode format are
  supported.
- **Bytecode produced by the luaugo compiler is byte-identical to the output
  of `luau --compile=binary`** for the same source and compile options.

## Scope

| Subsystem | Status |
|---|---|
| Lexer + Parser (AST) | port in progress |
| Bytecode encoder/decoder | port in progress |
| Compiler (AST &rarr; bytecode) | port in progress |
| Virtual machine (interpreter + GC) | port in progress |
| Standard library | port in progress |
| `luau` CLI (REPL + script runner) | port in progress |
| Static type checker (`luau-analyze`) | **out of scope** |
| Native code generator (JIT) | **out of scope** |

## Requirements

- Go **1.23** or newer.
- For differential conformance tests: the official `luau` binary on PATH.

## Building

```
go build ./...
go test ./...
```

## Project structure

```
cmd/luau/             REPL and script runner (matches upstream CLI flags)
internal/common/      Shared opcode and bytecode constants
pkg/ast/              Lexer + parser + AST nodes
pkg/bytecode/         Bytecode encoder, decoder, and Proto representation
pkg/compiler/         AST -> bytecode compiler with constant folding
pkg/vm/               Virtual machine: state, GC, interpreter, Lua C API
pkg/vm/lib/           Standard libraries (base, math, string, table, ...)
pkg/vm/builtins/      Fastcall builtin implementations
tests/conformance/    Conformance .luau scripts mirrored from upstream
tests/golden/         Reference .luac blobs produced by upstream luau
tools/                Maintenance scripts (upstream sync, golden refresh)
```

## License

luaugo is distributed under the terms of the [MIT License](LICENSE.txt).
Original Lua source is also under MIT (see `lua_LICENSE.txt`).
