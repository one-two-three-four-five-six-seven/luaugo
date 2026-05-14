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
swarm: bug-fix-batch-3 (active)
baseline: master @ 9f06895
agents:
  - fix-shims:
      target: register conformance-harness shim globals so the
              "attempt to call a nil value" cluster (buffers,
              coverage, debugger, integers_regspill, native,
              native_userdata, udata_direct, userdata) progresses
      worktree: .swarm/fix-shims
      owned:
        - tests/conformance_shims.go (NEW)
        - tests/luaugo_vm_suite_test.go
        - pkg/vm/lib/conformance_shims_test.go (NEW)
  - fix-tables-vector:
      target: tables.luau (table.insert arg check) + vector.luau
              (Magnitude/Unit property access)
      worktree: .swarm/fix-tables-vector
      owned:
        - pkg/vm/lib/table.go
        - pkg/vm/lib/vector.go
        - pkg/vm/lib/tables_insert_test.go (NEW)
        - pkg/vm/lib/vector_index_test.go (NEW)
        - pkg/vm/execute.go
  - fix-closure-timeout:
      target: closure.luau no longer TIMEOUT
      worktree: .swarm/fix-closure-timeout
      owned:
        - pkg/vm/closure.go
        - pkg/vm/do.go
        - pkg/vm/closure_loop_test.go (NEW)
```

## Recent swarm history

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
