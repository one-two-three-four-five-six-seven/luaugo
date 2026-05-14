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
swarm: bug-fix-batch-2 (active)
baseline: master @ 3e698b7
agents:
  - fix-basic:
      target: tests/conformance/basic.luau move RUNTIME_ERR -> OK
      worktree: .swarm/fix-basic
      owned:
        - pkg/vm/arith.go
        - pkg/vm/compare.go
        - pkg/vm/object.go
        - pkg/vm/basic_fix_test.go (NEW)
  - fix-tpack:
      target: tests/conformance/tpack.luau move RUNTIME_ERR -> OK
      worktree: .swarm/fix-tpack
      owned:
        - pkg/vm/lib/string.go
        - pkg/vm/lib/string_pack_test.go (NEW)
        - pkg/vm/lib/tpack_fix_test.go (NEW)
  - fix-pcall-errors:
      target: tests/conformance/pcall.luau and tests/conformance/errors.luau
      worktree: .swarm/fix-pcall-errors
      owned:
        - pkg/vm/do.go
        - pkg/vm/api.go
        - pkg/vm/auxlib.go
        - pkg/vm/lib/base.go
        - pkg/vm/pcall_fix_test.go (NEW)
        - pkg/vm/lib/errors_fix_test.go (NEW)
```

## Recent swarm history

(Filled in when a swarm completes.)
