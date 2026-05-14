# luaugo documentation

luaugo is a pure-Go implementation of the [Luau](https://github.com/luau-lang/luau)
programming language. It contains:

- a parser for Luau source code (`pkg/ast`),
- a compiler that emits Luau bytecode (`pkg/compiler`),
- a virtual machine that executes Luau bytecode (`pkg/vm`),
- the full Luau standard library (`pkg/vm/lib`),
- helpers for encoding and decoding `.luac` blobs (`pkg/bytecode`),
- a command-line front-end (`cmd/luau`).

The compiler produces bytecode that is **accepted by the upstream Luau VM**.
The luaugo VM is also able to execute that bytecode in-process without any
C dependency. There is no cgo and no third-party Go dependency: pure Go,
standard library only.

## Documents in this directory

| Document | Audience | What it covers |
|---|---|---|
| [getting-started.md](getting-started.md) | First-time users | Installation, "hello world", first script run. |
| [usage.md](usage.md) | Application developers | How to embed the compiler and VM, the Go API in detail. |
| [compatibility.md](compatibility.md) | Migrators from upstream Luau | What works, what doesn't, where behavior may differ. |
| [stdlib.md](stdlib.md) | Script authors | Quick reference for every function in the Luau standard library as exposed by luaugo. |
| [cli.md](cli.md) | Anyone running scripts | The `luau` command-line tool's flags and behavior. |
| [architecture.md](architecture.md) | Contributors | How the code is organized, where each subsystem lives. |
| [testing.md](testing.md) | Contributors | How to run the test suite, the differential harness, and the conformance runner. |
| [faq.md](faq.md) | Everyone | Common questions and gotchas. |

## Project status at a glance

- **53 / 53** upstream Luau conformance fixtures parse cleanly through the
  luaugo lexer + parser.
- **53 / 53** upstream Luau conformance fixtures compile through the luaugo
  compiler.
- **53 / 53** of those luaugo-compiled `.luac` blobs **load successfully on
  the official Luau VM**.
- **47 / 53** of them reach the same exit status as upstream-compiled bytecode.
- **45 / 53** produce byte-identical stdout to upstream-compiled bytecode.
- **All 86 Luau bytecode opcodes** are dispatched in the luaugo VM.
- **All 11 standard libraries** (base, math, string, table, coroutine,
  bit32, utf8, os, debug, buffer, vector) are implemented and exercised by
  93 passing in-process tests.
- **Race-detector clean** for the coroutine and pcall paths.

See [compatibility.md](compatibility.md) for the precise behavioral
guarantees and known divergences.
