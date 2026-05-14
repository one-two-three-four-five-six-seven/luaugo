# Testing luaugo

This document covers how to run the test suite and how the various
tests are organized.

## Quick start

From a clean checkout, the standard Go workflow works:

```
go build ./...
go vet ./...
go test ./...
```

This builds and tests every Go package and runs the entire conformance
suite. Expected result: every package green, zero failures, zero
skips on a machine that has the upstream `luau-compile` binary
available (see below).

For race-detector coverage on the coroutine and protected-call paths:

```
go test -race ./pkg/vm/... -timeout 120s
```

## Test layers

| Layer | Where | What it covers |
|---|---|---|
| Constants | `internal/common/opcodes_test.go` | Opcode enumeration matches upstream values; instruction encoders round-trip. |
| AST | `pkg/ast/conformance_parse_test.go` | All 53 conformance fixtures parse cleanly. |
| Bytecode codec | `pkg/bytecode/roundtrip_test.go`, `varint_test.go` | Synthetic and golden-blob round-trip identity; varint codec. |
| Compiler unit | `pkg/compiler/compiler_test.go` | 15 small end-to-end programs compile and execute correctly on the real Luau VM. |
| VM unit | `pkg/vm/vm_test.go`, `pkg/vm/execute_test.go`, `pkg/vm/builtins_test.go` | Object model, GC, stack ops, table ops, intern table, opcode dispatch, FASTCALL fast paths, coroutine yield/resume. |
| Standard library | `pkg/vm/lib/*_test.go` | 93 tests across 11 library files. |
| Conformance | `tests/conformance_suite_test.go` | The headline integration test: 53 upstream fixtures compiled by luaugo, executed on the upstream VM, compared against upstream-compiled equivalents. |
| Demo | `tools/demo/demo_test.go` | A single hand-written multi-feature script proving compiler + upstream VM works end to end. |

## Required external tooling

Some tests require auxiliary binaries. They all degrade gracefully
(`t.Skip` instead of `t.Fatal`) when the tool is absent, so a stock
`go test ./...` always passes.

### `luau-compile` (upstream)

Used by:
- `internal/upstreamvm` &mdash; helper that drives the differential
  harness.
- `tests/conformance_suite_test.go` &mdash; to produce reference
  bytecode for each fixture.
- `tools/demo/demo_test.go` &mdash; not strictly required because
  luaugo's compiler is used to produce the bytecode.

How luaugo finds it (in order):
1. `exec.LookPath("luau-compile")`.
2. `C:\Users\user\Downloads\luau-windows\luau-compile.exe` (workspace
   fallback used in this dev environment).
3. `C:\Users\user\Documents\luaugo\.upstream-bin\luau-compile.exe`.

To install on your own machine, download a release from
https://github.com/luau-lang/luau/releases and put it on `PATH`.

### `bcrunner` (luaugo's differential harness)

Used by:
- `internal/upstreamvm` &mdash; to actually execute bytecode on the
  upstream VM.
- `tests/conformance_suite_test.go`.
- `tools/demo/demo_test.go`.

This is a small C++ program in `tools/luau-bcrunner/` that links
statically against the upstream Luau VM source.

To build it once (Windows + MinGW):

```
powershell -File tools/luau-bcrunner/build.ps1
```

This compiles `tools/luau-bcrunner/bcrunner.exe` from
`bcrunner.cpp` plus every `.cpp` file under `.upstream/VM/src/`. The
binary is gitignored.

To rebuild after re-syncing upstream:

```
powershell -File tools/sync-upstream.ps1
powershell -File tools/luau-bcrunner/build.ps1
```

If `bcrunner.exe` is missing, the tests that need it print a `SKIP`
message naming the missing binary and explaining how to build it; they
do not fail.

## Running individual test groups

```
# Just the conformance suite
go test ./tests/ -run TestLuauConformanceSuite -v

# Just the compiler-level tests
go test ./pkg/compiler/ -v -count=1

# Just one standard library
go test ./pkg/vm/lib/ -v -run Math

# The race-clean coroutine tests
go test -race ./pkg/vm/lib/ -v -run Coroutine
```

## The conformance suite in detail

`TestLuauConformanceSuite` walks every fixture in `tests/conformance/`
and for each one performs four steps:

1. Compile the source with **upstream `luau-compile --binary`**. This
   produces the reference bytecode blob.
2. Compile the source with the **luaugo compiler**
   (`compiler.CompileBinary`). This produces our blob.
3. Execute the upstream blob via `bcrunner` on the **upstream Luau VM**
   with `--no-sandbox` and `--chunkname=$name`. Capture status and
   stdout.
4. Execute the luaugo blob via the same `bcrunner` invocation. Capture
   status and stdout.

The test reports four metrics:

- **Compile-clean** &mdash; how many fixtures the luaugo compiler
  accepts without raising a `CompileError`.
- **Load-OK** &mdash; how many luaugo blobs the upstream VM successfully
  loads (i.e. `luau_load` returns success).
- **Same status** &mdash; how many luaugo blobs reach the same VM exit
  status (OK, runtime error) as the upstream blob.
- **Same stdout** &mdash; how many produce identical stdout to upstream.

The **hard pass criterion** is Load-OK == 53/53. The other three
metrics are reported but do not gate.

Current numbers:
- Compile-clean: 53/53
- Load-OK: 53/53
- Same status: 47/53
- Same stdout: 45/53

The 6 status divergences and 8 stdout divergences are documented in
[compatibility.md](compatibility.md). They include unimplemented
optimizations (line-info emission, FASTCALL substitution) and a small
number of real compiler bugs being tracked.

## Adding a new test

### Unit tests

Add them in the appropriate package's `_test.go` file. Standard
Go conventions apply: use the `testing` package, name tests `TestXxx`,
use `t.Errorf` for assertions and `t.Fatalf` only when continuation is
impossible.

### Library tests that need a VM

The shape is:

```go
func TestSomething(t *testing.T) {
    s := vm.NewState()
    defer s.Close()
    lib.OpenBase(s)
    lib.OpenMath(s) // whichever libs your script needs

    blob, err := compiler.CompileBinary("test.luau",
        []byte(`return math.abs(-7)`), compiler.Defaults())
    if err != nil { t.Fatal(err) }

    if err := s.Load("test.luau", blob, 0); err != nil { t.Fatal(err) }
    if status := s.PCall(0, 1, 0); status != vm.StatusOK {
        msg, _ := s.ToString(-1)
        t.Fatalf("runtime: %s", msg)
    }
    n, _ := s.ToNumber(-1)
    if n != 7 { t.Errorf("got %v, want 7", n) }
}
```

### Tests that need the upstream VM

Use the `internal/upstreamvm` helper:

```go
func TestSomethingOnUpstream(t *testing.T) {
    upstreamvm.RequireAvailable(t) // skips if bcrunner is missing

    blob, err := compiler.CompileBinary("test.luau", source, compiler.Defaults())
    if err != nil { t.Fatal(err) }

    r, err := upstreamvm.Run(blob)
    if err != nil { t.Fatal(err) }
    if r.Status != upstreamvm.StatusOK {
        t.Fatalf("upstream rejected blob: stderr=%q", r.Stderr)
    }
    // Inspect r.Stdout
}
```

For comparison against upstream-compiled reference output:

```go
r, err := upstreamvm.RunSource(source) // compiles via luau-compile, then runs
```

## Continuous-integration-style sweep

The full check, equivalent to what CI would run:

```
go build ./...                            \
  && go vet ./...                         \
  && go test ./... -count=1 -timeout 300s \
  && go test -race ./pkg/vm/... -timeout 180s
```

Expected exit code 0 on a working tree with `luau-compile` and
`bcrunner.exe` available.
