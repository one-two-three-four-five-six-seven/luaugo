# luaugo — Pure-Go Luau VM and Compiler

> A drop-in [Luau](https://luau.org) runtime in pure Go. Embed Luau scripts in
> Go services. Run untrusted code in sandboxed VMs across goroutines. Compile,
> disassemble, and dump ASTs from the command line.

[![Go Reference](https://pkg.go.dev/badge/github.com/one-two-three-four-five-six-seven/luaugo.svg)](https://pkg.go.dev/github.com/one-two-three-four-five-six-seven/luaugo)
[![Go Version](https://img.shields.io/badge/go-1.23%2B-00ADD8?logo=go)](https://golang.org/dl/)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE.txt)
[![Conformance](https://img.shields.io/badge/conformance-50%2F50-brightgreen)](#conformance)
[![Pure Go](https://img.shields.io/badge/cgo-not%20required-success)](https://golang.org/cmd/cgo/)

### Tech Stack

![Go](https://img.shields.io/badge/go-%2300ADD8.svg?style=for-the-badge&logo=go&logoColor=white)
![Lua](https://img.shields.io/badge/lua-%232C2D72.svg?style=for-the-badge&logo=lua&logoColor=white)
![Roblox](https://img.shields.io/badge/Roblox-%230a0b0b.svg?style=for-the-badge&logo=Roblox&logoColor=white)
![JSON](https://img.shields.io/badge/JSON-000?style=for-the-badge&logo=json&logoColor=white)
![Markdown](https://img.shields.io/badge/markdown-%23000000.svg?style=for-the-badge&logo=markdown&logoColor=white)

### Built with

![Git](https://img.shields.io/badge/git-%23F05033.svg?style=for-the-badge&logo=git&logoColor=white)
![GitHub](https://img.shields.io/badge/github-%23121011.svg?style=for-the-badge&logo=github&logoColor=white)
![Claude](https://img.shields.io/badge/Claude-D97757?style=for-the-badge&logo=claude&logoColor=white)

**luaugo** is an independent, from-scratch reimplementation of the
[Luau](https://github.com/luau-lang/luau) programming language — Roblox's fast,
small, safe, gradually-typed scripting language derived from Lua 5.x — written
in idiomatic Go with zero third-party dependencies and zero cgo. It ships a
complete compiler, virtual machine, garbage collector, standard library, and
four CLI tools that mirror upstream's argument surface.

If you want to embed a Lua-family scripting language in a Go program without
linking against C, or you need to run thousands of isolated script instances
across goroutines, luaugo is for you.

---

## Table of contents

- [Why luaugo?](#why-luaugo)
- [Features](#features)
- [Install](#install)
- [Quick start (embedding)](#quick-start-embedding)
- [CLI tools](#cli-tools)
- [Massively concurrent scripting](#massively-concurrent-scripting)
- [Architecture](#architecture)
- [Conformance](#conformance)
- [What luaugo doesn't do](#what-luaugo-doesnt-do)
- [Comparison with alternatives](#comparison-with-alternatives)
- [Contributing](#contributing)
- [License](#license)

---

## Why luaugo?

| | luaugo | cgo-Luau bindings | gopher-lua |
|---|---|---|---|
| Pure Go | ✅ | ❌ | ✅ |
| Runs upstream `.luac` bytecode | ✅ (v3–v9) | ✅ | ❌ (Lua 5.1 only) |
| Emits bytecode upstream Luau can run | ✅ | ✅ | ❌ |
| Luau-specific syntax (types, `+=`, `\`, vec3, etc.) | ✅ | ✅ | ❌ |
| Concurrent VMs across goroutines | ✅ | ⚠ thread-locked | ✅ |
| Cross-compile to wasm/arm64/win | ✅ | ❌ (requires C toolchain) | ✅ |
| Standard library | base, math, string, table, coroutine, bit32, os, debug, buffer, vector, utf8 | full | partial |

`go get` and ship — no `gcc`, no `LUAU_INCLUDE_DIR`, no static archive
juggling, no platform-specific build matrix.

---

## Features

### Compatibility
- **Drop-in for upstream Luau bytecode.** Loads `.luac` blobs produced by the
  official `luau --compile=binary` compiler (versions 3 through 9). Bytecode
  produced by `luau-compile` (this repo's binary) round-trips through the
  upstream VM unchanged.
- **Full Luau syntax surface.** Type annotations, generics, `+= -= *= /= //= %= ^= ..=`,
  string interpolation backticks, `continue`, vector literals, `if-else`
  expressions, `type` and `export type` declarations, attributes (`@native`,
  `@deprecated`), the `\` line continuation, hex/binary numeric literals.
- **Conformance: 50/50 applicable upstream fixtures pass.** See
  [Conformance](#conformance) below for the methodology.

### Runtime
- **Independent VMs.** Each `*vm.State` is fully isolated — its own stack,
  globals, GC state, and string intern pool. Spin up thousands across
  goroutines without locking.
- **Coroutines as goroutines.** `coroutine.create` / `resume` / `yield` /
  `wrap` are implemented on top of Go's scheduler. Yield boundaries are
  cheap; chain depth is capped at 199 levels to match upstream's
  `LUAI_MAXCCALLS`.
- **Real garbage collector.** Tri-color incremental mark/sweep with weak
  tables, finalizers (`__gc`), and `collectgarbage("stop" / "restart" /
  "collect" / "count")` knobs wired to host APIs.
- **Sandboxing.** `Sandbox()` freezes the globals table; `SandboxThread()`
  gives a coroutine its own writeable globals with read-through `__index`
  fall-through — exactly upstream's `luaL_sandbox` / `luaL_sandboxthread`
  semantics.

### Standard library
- `base` — `print`, `tostring`, `tonumber`, `type`, `assert`, `pcall`,
  `xpcall`, `error`, `select`, `next`, `ipairs`, `pairs`, `rawget`, `rawset`,
  `rawequal`, `rawlen`, `setmetatable`, `getmetatable`, `collectgarbage`,
  `_VERSION`, `_G`, `require`-compatible loader hooks.
- `math` — full surface including `math.huge`, `math.pi`, `math.random`,
  `math.randomseed`, `math.noise` (Perlin), `math.fmod`, `math.modf`,
  `math.lerp`, `math.clamp`, `math.round`, `math.map`, `math.sign`.
- `string` — pattern matching, `string.format`, `string.gmatch`,
  `string.gsub`, `string.pack`, `string.unpack`, `string.split`.
- `table` — including `table.create`, `table.move`, `table.find`,
  `table.pack`, `table.unpack`, `table.clear`, `table.freeze`, `table.clone`,
  `table.sort` (introsort with shape-mutation detection).
- `coroutine` — `create`, `resume`, `yield`, `wrap`, `status`, `running`,
  `isyieldable`, `close`.
- `bit32` — full surface; `band`/`bor`/`bxor`/`bnot`/`btest`/`extract`/
  `replace`/`lshift`/`rshift`/`arshift`/`lrotate`/`rrotate`/`countlz`/`countrz`.
- `os` — `os.time`, `os.clock`, `os.date`, `os.difftime`.
- `debug` — `traceback`, `info`, `getinfo`, coverage hooks.
- `buffer` — `buffer.create`, `read*`, `write*` family for binary IO.
- `vector` — Luau's first-class `vector` type with arithmetic and metamethods.
- `utf8` — full UTF-8 codepoint surface.

---

## Install

```sh
# As a library in your Go project:
go get github.com/one-two-three-four-five-six-seven/luaugo

# As CLI tools (luau, luau-compile, luau-bytecode, luau-ast):
go install github.com/one-two-three-four-five-six-seven/luaugo/cmd/...@latest
```

**Requirements:** Go 1.23+. No C toolchain, no `cgo`, no external libraries.

---

## Quick start (embedding)

```go
package main

import (
    "fmt"

    "github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
    "github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
    "github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm/lib"
)

func main() {
    s := vm.NewState()
    defer s.Close()
    lib.OpenAll(s)

    // Register a Go function callable from Lua.
    s.Register("greet", func(state *vm.State) int {
        name, _ := state.ToString(1)
        fmt.Printf("hello, %s!\n", name)
        return 0
    })

    // Compile and run a Luau snippet.
    blob, _ := compiler.CompileBinary("=demo", []byte(`
        local name = ...
        greet(name)
        return string.upper(name), #name
    `), compiler.Defaults())

    s.Load("=demo", blob, 0)
    base := s.Top() - 1
    s.PushString("luaugo")
    if status := s.PCall(1, -1, 0); status != vm.StatusOK {
        msg, _ := s.ToString(-1)
        panic(msg)
    }

    upper, _ := s.ToString(base + 1)
    length, _ := s.ToInteger(base + 2)
    fmt.Printf("upper=%s length=%d\n", upper, length)
    // Output: hello, luaugo!
    //         upper=LUAUGO length=6
}
```

See [`docs/examples/embed`](docs/examples/embed) for a fully-annotated walk
through every embedding API: pushing/popping values, error handling,
coroutines, registering Go callbacks, sandboxing, and pre-compiled bytecode.

---

## CLI tools

Four binaries, each mirroring upstream's command-line surface so existing
build scripts and Makefiles work without modification.

### `luau` — script runner & REPL

```sh
luau script.luau                 # run a script
luau script.luau -a foo 42       # pass "foo" and "42" as varargs to the chunk
luau -i script.luau              # run, then drop into REPL with script's state
luau                             # interactive REPL
luau --coverage script.luau      # write coverage.out
```

Mirrors `Repl.cpp:649` upstream — same flags, same output formatting.
Accepts `--codegen*`, `--timetrace`, `--counters`, `--fflags=...` for build-
script compatibility (warns and continues; luaugo is interpreter-only).

### `luau-compile` — standalone compiler

```sh
luau-compile script.luau                          # default: --text disassembly
luau-compile --binary script.luau > out.luac      # emit bytecode blob
luau-compile --null script.luau                   # compile-only, prints timing
luau-compile -O2 -g2 --binary script.luau         # max optimization + debug info
```

Modes: `--text`, `--binary`, `--remarks`, `--null`, plus `--codegen*` variants
accepted with a warning. Supports `--vector-lib`, `--vector-ctor`,
`--vector-type` for custom vector bindings.

### `luau-bytecode` — opcode frequency analyzer

```sh
luau-bytecode --summary-file=out.json script.luau
```

Emits per-function opcode histograms as JSON. Same schema as upstream's
`luau-bytecode` so existing tooling parses the output unchanged.

### `luau-ast` — AST as JSON

```sh
luau-ast script.luau                  # parse, write AST JSON to stdout
echo 'return 1+1' | luau-ast -        # read source from stdin
```

Field names and node-type strings match upstream's `AstJsonEncoder`
(verified against `.upstream/Analysis/src/AstJsonEncoder.cpp`).

---

## Massively concurrent scripting

Because each `*vm.State` is fully isolated, luaugo lets you run thousands of
independent scripts across goroutines with near-linear scaling on multicore
hardware. The demo in [`docs/examples/swarm`](docs/examples/swarm) spins up
N worker goroutines, each owning its own VM, and feeds them an FNV-1a +
Murmur3 hashing workload via a channel:

```sh
go run ./docs/examples/swarm -vms 1000 -jobs 50000 -baseline
```

```
luaugo swarm demo
  GOMAXPROCS=16  vms=1000  jobs=50000

[parallel] 50000 jobs across 1000 VMs:  27.2s (1836 jobs/sec)
[baseline] 50000 jobs on 1 VM:          217.4s ( 230 jobs/sec)

speedup: 8.0x
```

**8.0x speedup on 16 cores**, with zero cross-VM contention and a checksum
that confirms parallel output is bit-for-bit identical to sequential. Real
workloads will see better numbers as job duration grows past goroutine
scheduling overhead.

This is the headline use case for luaugo over cgo Luau: the official C
library is not goroutine-safe, so concurrent embedders must either pool
states behind mutexes or pay the launch cost of `lua_newstate` per request.
luaugo states are Go objects — they cost a normal allocation, no syscalls.

---

## Architecture

```
cmd/
  luau/             # script runner + REPL          (mirrors upstream's `luau`)
  luau-compile/     # standalone compiler           (mirrors `luau-compile`)
  luau-bytecode/    # opcode histogram dumper       (mirrors `luau-bytecode`)
  luau-ast/         # AST-as-JSON dumper            (mirrors `luau-ast`)

pkg/
  ast/              # Lexer, parser, AST nodes, JSON encoder, pretty-printer
  bytecode/         # Bytecode reader/writer/disassembler (v3 through v9)
  compiler/         # AST → bytecode compiler with constant folding, inlining
  vm/               # Interpreter, GC, Lua C API surface, error machinery
  vm/lib/           # Standard library (base, math, string, table, …)
  vm/builtins/      # Fastcall builtin implementations

internal/
  common/           # Opcode and bytecode-layout constants (shared)
  clitool/          # CLI helpers (file globbing, BOM detection, --fflags stub)
  vmlog/            # LUAUGO_DEBUG=* gated tracing

docs/examples/      # Worked examples: embed, swarm (concurrent VMs)
tests/conformance/  # 53 fixtures mirrored from upstream's conformance suite
tools/              # Maintenance: upstream sync, golden refresh
.upstream/          # Git subtree of the official Luau source (reference only)
```

---

## Conformance

luaugo is validated against **53 fixtures** mirrored verbatim from upstream
Luau's conformance test suite (`tests/Conformance.test.cpp`). Each fixture is
compiled by luaugo's compiler and executed on luaugo's VM end-to-end.

**Current results:** 50/50 applicable fixtures pass. 3 fixtures are skipped
under the same rules upstream uses:

| Fixture | Upstream gate | Why luaugo skips |
|---|---|---|
| `integers.luau` | `FFlag::LuauIntegerType && FFlag::LuauIntegerLibrary` | Experimental separate-tag integer type, not in the stable VM. |
| `integers_regspill.luau` | `codegen && luau_codegen_supported()` | Native-codegen register-spill paths; no analogue in an interpreter. |
| `native_types.luau` | `codegen && luau_codegen_supported()` | Requires runtime CHECK_TAG guards emitted by the JIT. |

The suite is run as part of `go test ./...` and the gate is enforced: a
regression below 50/50 fails CI.

```sh
go test ./tests/ -run TestLuaugoVMSuite -v
```

---

## What luaugo doesn't do

To be explicit about scope:

- **No native code generation (JIT).** luaugo is interpreter-only. The
  upstream `CodeGen/` subsystem (~30k lines of ARM64/x86-64 IR lowering) is
  not ported. `luau-compile --codegen` accepts the flag and falls back to
  `--text` with a stderr warning.
- **No static type checker.** Upstream's `Analysis/` subsystem provides
  bidirectional type inference for the `luau-analyze` binary. luaugo's
  compiler parses type annotations but does not check them; runtime
  type-tag dispatch is the only type enforcement.
- **No `luau-reduce`.** The repro-reduction binary depends on the type
  checker.

Adding these is possible but each is a major separate effort. PRs welcome.

---

## Comparison with alternatives

**vs. [Shopify/go-lua](https://github.com/Shopify/go-lua):** go-lua is Lua
5.2 in pure Go. luaugo targets Luau (a different language with type
annotations, vector type, string interpolation, `+=` etc.) and is binary-
compatible with upstream Luau bytecode.

**vs. [yuin/gopher-lua](https://github.com/yuin/gopher-lua):** gopher-lua is
Lua 5.1 in pure Go. Not Luau-compatible: cannot run `.luac` files produced
by `luau`, cannot parse Luau-specific syntax.

**vs. cgo bindings to libluau:** Bindings require a C toolchain on every
build host, complicate cross-compilation, and serialize all VM access
through a single OS thread (Luau's C state is not goroutine-safe). luaugo
has none of those constraints.

**vs. WASM-compiled libluau:** Possible but adds a wasmtime/wazero runtime
to your binary, an FFI boundary on every Lua call, and significant memory
overhead per state. luaugo VMs are normal Go heap objects.

---

## Contributing

luaugo is an active port. Contributions are welcome, particularly:

- Filling stdlib gaps (run `go test ./tests/` and look for `RUNTIME_ERR`).
- Performance work — the interpreter has not been profile-guided yet.
- Better diagnostics — many parser/compiler errors can be more upstream-faithful.
- Native code generation, if you're brave.

Read [`tools/UPSTREAM_MAP.md`](tools/UPSTREAM_MAP.md) for the layout
correspondence between luaugo's Go packages and upstream's C++ source tree
before opening a PR.

Run the full test suite before submitting:

```sh
go test ./... -count=1
```

---

## License

luaugo is distributed under the [MIT License](LICENSE.txt).

Portions are derived from:
- [Luau](https://github.com/luau-lang/luau) © 2019–2026 Roblox Corporation, MIT License.
- [Lua](https://www.lua.org) © 1994–2019 Lua.org, PUC-Rio, MIT License.

Both upstream licenses are reproduced in [`lua_LICENSE.txt`](lua_LICENSE.txt)
and the project root.

---

<sub>**Keywords:** Luau Go, Luau interpreter Go, embed Lua Go, pure Go Lua VM, Roblox Lua Go, Luau pure Go, Luau bytecode VM, concurrent Lua scripting, Go scripting language, gopher-lua alternative, Luau without cgo, Luau compiler Go, Luau parser Go.</sub>
