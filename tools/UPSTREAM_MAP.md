# Upstream Luau source map

This document maps luaugo Go packages to the upstream C++ source files
they are ported from. Tag pin: see `tools/UPSTREAM.md`.

The upstream checkout lives in `.upstream/` after running
`tools/sync-upstream.ps1`.

## Go package -> upstream sources

### `internal/common`
- `.upstream/Common/include/Luau/Bytecode.h` (opcodes, constants, version
  enums, capability flags)
- `.upstream/Common/include/Luau/BytecodeUtils.h`
- `.upstream/Common/include/Luau/Common.h` (limits)

### `pkg/ast`
- `.upstream/Ast/src/Lexer.cpp` and `.upstream/Ast/include/Luau/Lexer.h`
- `.upstream/Ast/src/Parser.cpp` and `.upstream/Ast/include/Luau/Parser.h`
- `.upstream/Ast/src/Ast.cpp` and `.upstream/Ast/include/Luau/Ast.h`
- `.upstream/Ast/src/Location.cpp` and `.upstream/Ast/include/Luau/Location.h`
- `.upstream/Ast/src/Confusables.cpp` and `.upstream/Ast/src/StringUtils.cpp`

### `pkg/bytecode`
- `.upstream/Bytecode/include/Luau/BytecodeBuilder.h`
- `.upstream/Bytecode/src/BytecodeBuilder.cpp` (writer)
- `.upstream/VM/src/lvmload.cpp` (reader)
- `.upstream/Common/include/Luau/Bytecode.h` (binary layout)

### `pkg/compiler`
- `.upstream/Compiler/include/Luau/Compiler.h`
- `.upstream/Compiler/src/Compiler.cpp`
- `.upstream/Compiler/src/Builtins.cpp` / `.h`
- `.upstream/Compiler/src/BuiltinFolding.cpp` / `.h`
- `.upstream/Compiler/src/ConstantFolding.cpp` / `.h`
- `.upstream/Compiler/src/CostModel.cpp` / `.h`
- `.upstream/Compiler/src/TableShape.cpp` / `.h`
- `.upstream/Compiler/src/Types.cpp` / `.h`
- `.upstream/Compiler/src/ValueTracking.cpp` / `.h`
- `.upstream/Compiler/src/lcode.cpp` (luau_compile C entry point)

### `pkg/vm`
- `.upstream/VM/src/lstate.cpp` / `.h` (per-state, global state)
- `.upstream/VM/src/lobject.cpp` / `.h` (TValue, GCObject)
- `.upstream/VM/src/ltable.cpp` / `.h` (hybrid array+hash table)
- `.upstream/VM/src/lstring.cpp` / `.h` (intern table, atoms)
- `.upstream/VM/src/lfunc.cpp` / `.h` (closures, upvalues, prototypes)
- `.upstream/VM/src/ludata.cpp` / `.h` (userdata)
- `.upstream/VM/src/lbuffer.cpp` / `.h` (buffer type)
- `.upstream/VM/src/lgc.cpp` / `.h` and `.upstream/VM/src/lgcdebug.cpp`
- `.upstream/VM/src/ltm.cpp` / `.h` (tag methods / metamethods)
- `.upstream/VM/src/lmem.cpp` / `.h` (allocation; mostly a no-op in Go)
- `.upstream/VM/src/ldo.cpp` / `.h` (call/return/error/yield)
- `.upstream/VM/src/lapi.cpp` / `.h` (Lua C API)
- `.upstream/VM/src/laux.cpp` (luaL_* helpers)
- `.upstream/VM/src/lbuiltins.cpp` / `.h` (fastcall builtins)
- `.upstream/VM/src/ldebug.cpp` / `.h`
- `.upstream/VM/src/lnumprint.cpp` / `.upstream/VM/src/lnumutils.h`
- `.upstream/VM/src/lvmload.cpp` (bytecode loader)
- `.upstream/VM/src/lvmexecute.cpp` (interpreter loop)
- `.upstream/VM/src/lvmutils.cpp` (concat, equality, less-than, arith)
- `.upstream/VM/src/linit.cpp` (luaL_openlibs)

### `pkg/vm/lib`
- `pkg/vm/lib/base.go`     <- `.upstream/VM/src/lbaselib.cpp`
- `pkg/vm/lib/math.go`     <- `.upstream/VM/src/lmathlib.cpp`
- `pkg/vm/lib/string.go`   <- `.upstream/VM/src/lstrlib.cpp`
- `pkg/vm/lib/table.go`    <- `.upstream/VM/src/ltablib.cpp`
- `pkg/vm/lib/coroutine.go`<- `.upstream/VM/src/lcorolib.cpp`
- `pkg/vm/lib/bit32.go`    <- `.upstream/VM/src/lbitlib.cpp`
- `pkg/vm/lib/utf8.go`     <- `.upstream/VM/src/lutf8lib.cpp`
- `pkg/vm/lib/os.go`       <- `.upstream/VM/src/loslib.cpp`
- `pkg/vm/lib/debug.go`    <- `.upstream/VM/src/ldblib.cpp`
- `pkg/vm/lib/buffer.go`   <- `.upstream/VM/src/lbuflib.cpp`
- `pkg/vm/lib/vector.go`   <- `.upstream/VM/src/lveclib.cpp`
- (`lintlib.cpp` if present is folded into base or math; check upstream
   `linit.cpp` for which libs are opened by default.)

### `cmd/luau`
- `.upstream/CLI/src/Repl.cpp`, `Reduce.cpp`, etc. (use as reference; we
  reimplement REPL in idiomatic Go using `bufio` + a minimal line editor).

## Out of scope (do not port)

- `.upstream/CodeGen/**` (native JIT, x64/A64)
- `.upstream/Analysis/**` (type checker, linter)
- `.upstream/Config/**` (`.luaurc` parsing; deferred)
- `.upstream/Require/**` (module resolver; deferred)
- `.upstream/extern/**` (third-party deps like isocline, doctest)
- `.upstream/fuzz/**`
- `.upstream/bench/**`
- `.upstream/tools/**` (developer tools, not language runtime)
