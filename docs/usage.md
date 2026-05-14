# Embedding luaugo in a Go application

This document is the reference for the public Go API. It assumes you are
comfortable with Go and have read [getting-started.md](getting-started.md).

The luaugo public API is intentionally small and idiomatic. The major
packages are:

```
github.com/one-two-three-four-five-six-seven/luaugo/pkg/ast        // Lexer, parser, AST node types
github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler   // AST -> bytecode
github.com/one-two-three-four-five-six-seven/luaugo/pkg/bytecode   // bytecode encoder/decoder/disassembler
github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm         // virtual machine, Lua C API equivalents
github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm/lib     // standard library (base, math, string, ...)
```

You can pull in only the pieces you need. For example, an embedder that
ships precompiled bytecode and only needs to run it can depend on
`pkg/vm` and `pkg/vm/lib` alone, with no parser or compiler in the
binary.

## Lifecycle of a script

```
source bytes
    | ast.Parse            -> *ast.Program
    | compiler.Compile     -> *bytecode.Module
    | bytecode.Encode      -> []byte (.luac)
    | vm.State.Load        -> closure on the VM stack
    | vm.State.Call        -> execute the closure
```

`compiler.CompileBinary` collapses the first three steps into one call,
and `compiler.CompileSource` collapses Parse + Compile but skips the
final binary encoding so you can manipulate the in-memory `Module`.

## Compiling source

```go
import "github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"

opts := compiler.Defaults() // -O1, -g1 -- matches upstream `luau-compile`.
blob, err := compiler.CompileBinary("script.luau", source, opts)
if err != nil {
    // CompileError is a *compiler.CompileError carrying ast.Location.
    return err
}
```

`compiler.Options` exposes the following knobs (defaults shown):

| Field | Default | Meaning |
|---|---|---|
| `OptimizationLevel` | `OptBaseline` (1) | 0 = none, 1 = baseline (folding off; see [compatibility.md](compatibility.md)), 2 = aggressive. |
| `DebugLevel` | `DebugLines` (1) | 0 = none, 1 = line info, 2 = full (local + upvalue names). |
| `CoverageLevel` | `CoverageOff` (0) | Instruction-level coverage instrumentation. |
| `TypeInfoLevel` | `TypeInfoNone` (0) | Emit type info section; the current compiler emits an empty section regardless. |
| `VectorLib`, `VectorCtor`, `VectorType` | `""` | When non-empty, calls to `VectorLib.VectorCtor(x,y,z)` fold to vector constants. |
| `MutableGlobals` | `nil` | Names of globals the compiler must not treat as read-only. |
| `UserdataTypes` | `nil` | Host-registered userdata type names, used for type-info emission. |

The current compiler accepts every option but does not yet implement the
optimizations gated by `OptAggressive`; see [compatibility.md](compatibility.md).

## Creating a VM state

```go
import (
    "github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
    "github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm/lib"
)

s := vm.NewState()
defer s.Close()
lib.OpenAll(s) // opens base, math, string, table, coroutine, bit32,
               // utf8, os, debug, buffer, vector
```

If you do not want every library, open only what you need:

```go
lib.OpenBase(s)
lib.OpenMath(s)
lib.OpenString(s)
// ...
```

The functions are independent; each one only registers its own table or
globals.

## Loading and running bytecode

```go
if err := s.Load("script.luau", blob, 0); err != nil {
    return err // load-time error from luau_load equivalent
}
// The top of the stack is now the loaded chunk (a function value).

switch s.PCall(0, vm.MultRet, 0) {
case vm.StatusOK:
    // Returned values are on the stack from index 1 onward.
case vm.StatusErrRun:
    msg, _ := s.ToString(-1)
    return fmt.Errorf("runtime error: %s", msg)
}
```

`vm.MultRet` (= -1) tells the VM to keep every return value the script
produced. Pass a non-negative number to fix the count instead.

If you want errors as Go panics (rare; better only for prototypes), use
`s.Call(nargs, nresults)` instead of `PCall`. Any runtime error during a
non-protected call propagates as a Go panic carrying a `vm.Error`.

## The stack and the Lua C API

Following Lua convention, every value passed between Go and Luau lives on
the VM's value stack. Stack indices are 1-based from the bottom; negative
indices count from the top.

Reading and writing values:

```go
s.PushNumber(3.14)
s.PushString("hello")
s.PushBoolean(true)
s.NewTable()
s.PushInteger(42)

// Stack now: [3.14, "hello", true, {}, 42]
fmt.Println(s.Top())               // 5
n, _ := s.ToNumber(1)              // 3.14
str, _ := s.ToString(2)            // "hello"
b := s.ToBoolean(3)                // true
_ = s.Type(4)                      // vm.TTable
i, _ := s.ToInteger(-1)            // 42

s.Pop(2)                           // stack now: [3.14, "hello", true]
```

Tables:

```go
s.NewTable()
s.PushString("name")
s.PushString("luaugo")
s.RawSet(-3)                  // t.name = "luaugo", pops key and value
s.PushString("version")
s.PushInteger(1)
s.RawSet(-3)
// stack: [t]

s.GetField(-1, "name")        // pushes t.name; metamethods invoked.
name, _ := s.ToString(-1)
s.Pop(1)
fmt.Println(name)             // "luaugo"
```

Function calls:

```go
s.GetGlobal("print")
s.PushString("hello from Go")
status := s.PCall(1, 0, 0)    // 1 arg, 0 results
if status != vm.StatusOK {
    ...
}
```

## Registering Go functions

The simplest way to expose Go code to Luau:

```go
s.Register("double", func(s *vm.State) int {
    n, ok := s.ToNumber(1)
    if !ok {
        s.Errorf("double: expected number, got %s", s.Type(1))
    }
    s.PushNumber(n * 2)
    return 1 // number of return values left on the stack
})
```

A Luau script can now call `double(21)` and observe `42`.

Inside a Go callback you are running on the VM's call stack:

- Argument 1 is at stack index 1, argument 2 at index 2, etc.
- `Top()` returns the number of arguments received.
- Return the number of values you left on the stack (any leading args you
  did not consume are *not* returned automatically; pop or replace them).
- To raise a Luau error from Go, call `s.Error()` (with the error value
  already pushed) or `s.Errorf(format, args...)`. Both unwind back to the
  enclosing `PCall`.

To attach a function to a table other than `_G`, build the table first
then write the field:

```go
s.NewTable()
s.PushGoFunction(myCtor, 0)
s.SetField(-2, "create")
s.PushGoFunction(myDel, 0)
s.SetField(-2, "destroy")
s.SetGlobal("myapi")           // _G.myapi = {create=..., destroy=...}
```

## Errors and `pcall`

Runtime errors flow through Go in two complementary ways:

1. **From Go into Luau**: call `s.Error()` (with an error value pre-pushed)
   or `s.Errorf(format, args...)`. These do not return; they unwind back
   to the enclosing protected call boundary.
2. **From Luau into Go**: use `s.PCall(nargs, nresults, errfunc)` instead
   of `s.Call`. A non-`StatusOK` status leaves the error value on top of
   the stack. To map it to a Go error:

   ```go
   if st := s.PCall(0, vm.MultRet, 0); st != vm.StatusOK {
       msg, _ := s.ToString(-1)
       s.Pop(1)
       return fmt.Errorf("script %s: %s", chunkname, msg)
   }
   ```

`xpcall` is exposed as a global to Luau code in the standard `base`
library; no Go-side wiring is necessary.

## Coroutines

Coroutines are first-class Lua values; from the Go side you usually only
need to know they exist for the `vm.TThread` type tag. From inside Luau,
the `coroutine` library works the same way as in upstream Luau.

If you do need to drive a coroutine from Go:

```go
co := s.NewThread()              // pushes the new thread; returns *State
co.PushGoFunction(myWork, 0)     // body of the coroutine
status := co.Resume(s, 0)        // run until yield/return
```

`Yield(nresults)` from inside a Go callback running on a coroutine
suspends until the next `Resume`.

The implementation uses one goroutine per coroutine plus a per-VM
scheduler mutex; only one coroutine of a given state runs at a time, so
the visible semantics match Lua exactly. Race-detector clean.

## Reading and writing bytecode directly

If you have bytecode bytes from somewhere else (a Roblox export, an
upstream `luau-compile` invocation, an asset file), you do not need
luaugo's compiler to run it:

```go
import "os"
import "github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
import "github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm/lib"

blob, _ := os.ReadFile("game.luac")
s := vm.NewState()
defer s.Close()
lib.OpenAll(s)
s.Load("game.luac", blob, 0)
s.PCall(0, 0, 0)
```

To inspect bytecode without executing it:

```go
import "github.com/one-two-three-four-five-six-seven/luaugo/pkg/bytecode"

m, err := bytecode.Decode(blob)
if err != nil { ... }
fmt.Println(bytecode.Disassemble(m))
```

To produce a `.luac` blob without going through the disk:

```go
import "github.com/one-two-three-four-five-six-seven/luaugo/pkg/bytecode"
import "github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"

src := []byte(`return 1 + 2`)
mod, err := compiler.CompileSource("inline.luau", src, compiler.Defaults())
if err != nil { ... }

bytes, err := bytecode.Encode(mod, bytecode.EncodeOptions{})
if err != nil { ... }
// bytes is suitable for vm.State.Load or for the upstream Luau VM.
```

## Sandboxing

```go
s := vm.NewState()
lib.OpenAll(s)
s.Sandbox()             // freeze the global table; subsequent writes
                        // to existing globals raise an error.
                        // New globals are still permitted.
s.SandboxThread()       // each coroutine gets its own globals table
                        // copy-on-write from the parent.
```

These two methods mirror upstream Luau's `luaL_sandbox` and
`luaL_sandboxthread`. Calling them before loading untrusted scripts
prevents accidental clobbering of `print`, `math`, etc.

## Garbage collection

luaugo runs its own incremental tri-color mark-and-sweep collector (it
does not rely on Go's GC for Luau-visible lifetimes; this preserves the
Lua semantics for weak tables, `table.freeze`, finalizers, and the
`gcinfo()` builtin).

The relevant `*State` methods:

| Method | Purpose |
|---|---|
| `s.GCInfo() int` | Bytes-on-heap reported in kilobytes, matching Lua's `gcinfo()`. |
| `s.CollectGarbage()` | Run a full GC cycle. |

In normal operation you never need to call `CollectGarbage` &mdash; the
incremental collector runs at safepoints (return, jump-back, table and
closure allocation).

## Threading model

A single `*vm.State` is **not** safe for concurrent use from multiple Go
goroutines. If you need to call into the same Luau state from several
goroutines, gate the calls with a `sync.Mutex` of your own. To run
several Luau programs in parallel, create multiple independent `*State`
values; they do not share any state.

Coroutines of the same `*State` *are* allowed to run interleaved on
different goroutines, but the runtime enforces (via an internal mutex)
that only one of them is executing at a time. This matches upstream Lua's
cooperative-coroutine semantics.

## Putting it together: a runnable example

```go
package main

import (
    "fmt"
    "os"

    "github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
    "github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
    "github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm/lib"
)

func main() {
    s := vm.NewState()
    defer s.Close()
    lib.OpenAll(s)

    s.Register("double", func(s *vm.State) int {
        n, ok := s.ToNumber(1)
        if !ok {
            s.Errorf("double: expected number")
        }
        s.PushNumber(n * 2)
        return 1
    })

    src := []byte(`
        local function sum(t)
            local s = 0
            for _, v in ipairs(t) do s = s + v end
            return s
        end
        local nums = {1, 2, 3, 4, 5}
        return double(sum(nums))
    `)

    blob, err := compiler.CompileBinary("example.luau", src, compiler.Defaults())
    if err != nil {
        fmt.Fprintln(os.Stderr, "compile:", err)
        os.Exit(1)
    }
    if err := s.Load("example.luau", blob, 0); err != nil {
        fmt.Fprintln(os.Stderr, "load:", err)
        os.Exit(1)
    }
    if status := s.PCall(0, 1, 0); status != vm.StatusOK {
        msg, _ := s.ToString(-1)
        fmt.Fprintln(os.Stderr, "runtime:", msg)
        os.Exit(1)
    }

    result, _ := s.ToNumber(-1)
    fmt.Println("double(sum({1,2,3,4,5})) =", result) // 30
}
```
