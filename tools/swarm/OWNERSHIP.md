# File ownership map

When dispatching a swarm, the orchestrator updates the table below to
declare which agent owns which files. This file is the contract: an
agent may only modify files inside its owned set, and no two agents
may own overlapping files.

The integrator (`integrate.ps1`) reads this file and rejects any agent
whose diff touches a file outside its owned set.

## Always-shared (read-only for every agent)

- `.upstream/**`
- `tests/conformance/**.lua{,u}`
- `tools/luau-bcrunner/**`
- `internal/vmlog/**`
- `internal/upstreamvm/**` (helper API; do not modify)
- `tests/fixtures_test.go`, `tests/conformance_suite_test.go`,
  `tests/luaugo_vm_suite_test.go`, `tests/luaugo_vm_smoke_test.go`

## Orchestrator-only (never touched by any agent)

- `**/contract.go`
- `internal/common/**`
- `go.mod`, `go.sum`
- `tools/swarm/**` (this directory)
- `docs/**`
- `LICENSE.txt`, `lua_LICENSE.txt`, `README.md`, `STYLE.md`,
  `CONTRIBUTING.md`

## Active swarm assignments

Updated by the orchestrator at the start of each swarm. Empty when no
swarm is running.

```
swarm: (none active)
baseline: (n/a)
agents: []
```

## Recent swarm history

### bug-fix-batch-4 (completed at master @ 0fc6bd2)

baseline: master @ 6a3fbc2

| agent | target | files changed | outcome |
|---|---|---|---|
| fix-basic-compiler | basic.luau:188 numeric-for layout | pkg/compiler/compiler.go + new test | Real bug fixed (3-reg -> 4-reg layout). Fixture advances to line 250 (next bug). |
| fix-events-arith | events.luau:475 | pkg/vm/execute.go (out-of-scope but kept) + new test | Real bug fixed (GETGLOBAL/SETGLOBAL/GETIMPORT now invoke __index/__newindex). Fixture advances to line 487. |
| fix-pcall-redo | pcall.luau:8 + errors.luau:198 | pkg/vm/lib/{base,debug}.go + new tests | debug.info(C-fn,"s")="[C]" fixed; xpcall traceback format fixed. Fixtures advance to lines 129 / 192. |

Net: 30/53 -> 30/53 (deep-bug fixes; fixtures hit secondary issues).
Zero conflicts.

### bug-fix-batch-3 (completed at master @ 6a3fbc2)

baseline: master @ 9f06895

| agent | target | files changed | outcome |
|---|---|---|---|
| fix-shims | conformance-harness globals | tests/conformance_shims.go (NEW) + new tests | +2 fixtures (buffers, debugger). |
| fix-tables-vector | tables.luau + vector.luau | pkg/vm/lib/{table,vector}.go + tests | Real bugs fixed; fixtures advance. |
| fix-closure-timeout | closure.luau TIMEOUT | new diagnostic test only | Diagnostic; root cause in thread.go (outside scope). Orchestrator applied 16-line fix directly post-swarm: 29 -> 30. |

### bug-fix-batch-2 (completed at master @ 0fa87d0)

baseline: master @ 3e698b7

| agent | target | files changed | outcome |
|---|---|---|---|
| fix-basic | basic.luau | pkg/vm/arith.go + new test | Real bug fixed (concat wording). Fixture still RUNTIME_ERR at line 188 (out-of-scope compiler bug). |
| fix-tpack | tpack.luau | pkg/vm/lib/string.go + new tests | Fixture flipped to OK. Four pack/unpack bugs fixed. |
| fix-pcall-errors | pcall.luau, errors.luau | pkg/vm/do.go + new tests | Two real bugs fixed (recursion off-by-one, error prefix). Fixtures still RUNTIME_ERR (out-of-scope debug-lib gaps). |

Net suite-wide change: 26/53 -> 27/53. Zero agent conflicts.
Coordination layer (isolated worktrees + ownership map) proved out.

## Recent swarm history

(Filled in when a swarm completes.)
