# Getting started with luaugo

## Requirements

- **Go 1.23 or newer.** luaugo uses generics and a few stdlib additions
  that landed in 1.23.
- **No C compiler is needed to use luaugo.** The library and CLI are
  pure Go.
- A C++17 compiler (such as `g++` from MinGW) is only needed if you want
  to build the optional `bcrunner` differential-test harness that links
  against upstream Luau VM sources.

## Building from source

Clone the repository and build everything:

```
git clone https://github.com/one-two-three-four-five-six-seven/luaugo
cd luaugo
go build ./...
```

This compiles every package in the module, including the CLI under
`cmd/luau`. The first build also pulls down nothing else &mdash; there are
no third-party module dependencies.

To run the tests, including the integration test that drives the upstream
Luau VM, see [testing.md](testing.md).

## A first script

Save the following as `hello.luau`:

```lua
local function greet(name)
    return "Hello, " .. name .. "!"
end

print(greet("luaugo"))
return 42
```

There are three ways to run it.

### 1. Run via the luaugo CLI

The CLI takes the source file as its argument and executes it on the
luaugo VM:

```
go run ./cmd/luau hello.luau
```

The CLI is intentionally minimal; see [cli.md](cli.md) for its flags.

### 2. Compile and run in two steps

If you want a deployable `.luac` blob, compile first:

```
go run ./cmd/luau --compile=binary hello.luau > hello.luac
```

This produces a Luau bytecode blob compatible with the **official Luau
VM**. You can run it through any bytecode loader that speaks bytecode
versions 3 through 9. For example, with upstream `luau` and `luau-compile`
on your `PATH`, the same blob runs unchanged:

```
luau-compile --binary hello.luau > upstream.luac
# Or, equivalently from luaugo:
go run ./cmd/luau --compile=binary hello.luau > luaugo.luac
# Either blob is acceptable to the official luau runner.
```

### 3. Embed luaugo in a Go program

The most common usage is calling the compiler and VM from inside a Go
program. Here is the minimum needed to compile and execute a snippet:

```go
package main

import (
    "fmt"

    "github.com/one-two-three-four-five-six-seven/luaugo/pkg/compiler"
    "github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm"
    "github.com/one-two-three-four-five-six-seven/luaugo/pkg/vm/lib"
)

func main() {
    source := []byte(`
        local function greet(name) return "Hello, " .. name .. "!" end
        print(greet("luaugo"))
        return 42
    `)

    // 1. Compile source to bytecode.
    blob, err := compiler.CompileBinary("hello.luau", source, compiler.Defaults())
    if err != nil {
        panic(err)
    }

    // 2. Create a VM state with the standard libraries opened.
    s := vm.NewState()
    defer s.Close()
    lib.OpenAll(s)

    // 3. Load and run the bytecode.
    if err := s.Load("hello.luau", blob, 0); err != nil {
        panic(err)
    }
    if status := s.PCall(0, 1, 0); status != vm.StatusOK {
        msg, _ := s.ToString(-1)
        panic(fmt.Errorf("luau error: %s", msg))
    }

    // 4. Read the return value.
    n, _ := s.ToNumber(-1)
    fmt.Println("script returned:", n)
}
```

Expected output:

```
Hello, luaugo!
script returned: 42
```

## Where to next

- The [usage guide](usage.md) walks through the embedding API in full,
  with examples for passing values between Go and Luau, registering Go
  functions as callable globals, handling errors, and running coroutines.
- The [stdlib reference](stdlib.md) lists every standard-library function
  Luau scripts can call.
- The [compatibility document](compatibility.md) describes precisely
  where luaugo's behavior matches and diverges from upstream Luau.
