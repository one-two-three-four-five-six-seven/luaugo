# The `luau` command-line tool

`cmd/luau` is the luaugo command-line front-end. It mirrors a small,
deliberately conservative subset of upstream Luau's CLI surface.

## Building the CLI

From the repository root:

```
go build -o bin/luau ./cmd/luau
```

This produces `bin/luau` (or `bin\luau.exe` on Windows). The binary is
fully self-contained and statically linked &mdash; you can copy it to
another machine of the same OS and architecture and it will run.

You can also invoke it without installing:

```
go run ./cmd/luau <args>
```

## Synopsis

```
luau [--compile=MODE] [-O<n>] [-g<n>] [--version] [script.luau]
```

## Flags

Flag | Default | Meaning
--- | --- | ---
`--version` | off | Print version string and exit.
`--compile=MODE` | unset | Compile and emit instead of executing. Currently the only supported `MODE` is `binary`, which writes the `.luac` bytecode blob to stdout. Without this flag the script is executed.
`-O<n>` | `1` | Optimization level. `0` = none, `1` = baseline (default; matches upstream `luau-compile`), `2` = aggressive. See [compatibility.md](compatibility.md) for which optimizations are currently a no-op.
`-g<n>` | `1` | Debug-info level. `0` = none, `1` = line info, `2` = full (local + upvalue names). Note: the luaugo compiler does not yet emit line info; this flag accepts the value but currently produces empty debug sections.

## Behavior

### With no arguments

Starts a REPL placeholder that prints an instructive message; a full
REPL is not yet wired up.

### `luau script.luau`

1. Reads `script.luau` from disk.
2. Compiles it to bytecode with `compiler.Defaults()`.
3. Loads it on a fresh `*vm.State` with `lib.OpenAll` invoked.
4. Calls the main chunk under `PCall`.
5. Prints any return values to stdout, tab-separated, one line.
6. Exits with code `0` on success or `1` on a runtime error.

### `luau --compile=binary script.luau`

1. Reads `script.luau`.
2. Compiles it with `compiler.Defaults()`.
3. Writes the resulting bytecode bytes to stdout.

This output is suitable for redirection into a `.luac` file:

```
luau --compile=binary script.luau > script.luac
```

The bytes are accepted by the official upstream Luau VM. You can verify
on a machine that has upstream `luau` installed:

```
# Round-trip: luaugo compiles, upstream runs.
luau --compile=binary hello.luau > hello.luac
# The upstream "luau" runner only accepts source files, but the
# upstream VM is happy to load the blob via tools/luau-bcrunner (see
# docs/testing.md) or any embedder that calls luau_load().
```

## Exit codes

Code | Meaning
--- | ---
`0` | Successful compile or successful execution.
`1` | I/O error (file not found, write failure) or runtime error.
`2` | Unimplemented flag (currently `--compile=text` and `--compile=remarks`).

## Comparison to upstream `luau`

The upstream `luau` binary is a much larger tool that includes the type
analyzer, native codegen, profiling helpers, and an interactive REPL.
luaugo's CLI deliberately keeps a minimal surface:

| Upstream `luau` feature | luaugo CLI |
|---|---|
| Script execution | Yes |
| `--compile=binary` | Yes |
| `--compile=text` (disassembly) | Pending |
| `--compile=remarks` | Not planned |
| `--codegen` (native) | Not planned |
| `--coverage` | Pending |
| Interactive REPL | Pending |
| `-O0` / `-O1` / `-O2` | Accepted (semantics described above) |
| `-g0` / `-g1` / `-g2` | Accepted |
| Argument passing via `-a` | Pending |

For workflows that need analysis or REPL, install the official upstream
`luau` and `luau-analyze` and use them alongside luaugo. The two
implementations are interoperable at the bytecode level.

## Example session

```
$ cat > hello.luau <<'EOF'
> local function greet(name)
>     return "Hello, " .. name .. "!"
> end
> print(greet("luaugo"))
> return 42
> EOF

$ go build -o bin/luau ./cmd/luau

$ ./bin/luau hello.luau
Hello, luaugo!
42

$ ./bin/luau --compile=binary hello.luau > hello.luac
$ wc -c hello.luac
156 hello.luac
$ xxd hello.luac | head -2
00000000: 0901 0570 7269 6e74 0568 656c 6c6f 200d  ...print.hello .
00000010: 0e1a 1801 0000 0000 0700 0000 0701 0000  ................
```

The leading `09` byte is the bytecode version (`v9`, matching upstream
`luau-compile`'s output).
