# Swarm coordination

The luaugo bug-fix swarms repeatedly clobbered each other because every
agent worked in the same Git working tree from the same starting
snapshot. Two agents modifying `pkg/vm/execute.go` would race: whichever
wrote second overwrote the other's fix.

This directory is the coordination layer that prevents that.

## Mechanism (three layers)

### 1. Per-agent isolated worktrees

Each agent runs in its own `git worktree` rooted at
`.swarm/<agent-id>/` off the main repo. The agent's prompt tells it to
work only in its worktree. Other agents' worktrees are invisible to it
(they sit in different on-disk directories).

`tools/swarm/spawn.ps1` creates worktrees on demand:

```
powershell -File tools/swarm/spawn.ps1 -AgentId fix-basic -Baseline HEAD
```

This creates `.swarm/fix-basic/` checked out at the current `HEAD`. The
agent's prompt should say:

> WORKSPACE: C:\Users\user\Documents\luaugo\.swarm\fix-basic
> (this is your isolated worktree; do NOT touch any path outside it)

### 2. File ownership map

`OWNERSHIP.md` is the source of truth for who touches what. When
dispatching a swarm the orchestrator:

1. Picks disjoint file sets per agent.
2. Encodes them in `OWNERSHIP.md`.
3. Each agent's prompt explicitly lists its owned-files set.
4. The integrator (`tools/swarm/integrate.ps1`) verifies each agent's
   diff stays inside its allowed set, then merges.

Files that EVERY agent might need to read (but NOT modify):
- `.upstream/**` -- upstream Luau source for reference.
- `tests/conformance/**` -- fixtures.
- `tools/luau-bcrunner/bcrunner.exe` -- differential harness.
- `internal/vmlog/**` -- logging tool.

Files that are NEVER modifiable by any agent (orchestrator-only):
- `**/contract.go` -- locked surface.
- `internal/common/**` -- shared opcode/version constants.
- `go.mod`, `go.sum`.
- `tools/swarm/**` -- this coordination layer.

### 3. Frozen baseline

Every agent gets a baseline commit hash in its prompt and is told to
treat that hash as ground truth. They read upstream source and unrelated
files from that snapshot; they do NOT inspect sibling agents' worktrees.

## Integration workflow

```
# Orchestrator side (this directory):
powershell -File tools/swarm/spawn.ps1 -AgentId A1 -Baseline HEAD
powershell -File tools/swarm/spawn.ps1 -AgentId A2 -Baseline HEAD
powershell -File tools/swarm/spawn.ps1 -AgentId A3 -Baseline HEAD

# Dispatch each agent via the `task` tool with WORKSPACE pointed at
# its worktree. Agents run in parallel.

# After all agents return:
powershell -File tools/swarm/integrate.ps1 -AgentIds A1,A2,A3
# This:
#   1. Verifies each worktree's diff stays inside its allowed file set.
#   2. Cherry-picks each worktree's commit onto master (sequentially).
#   3. Runs go build / vet / test after each merge.
#   4. Aborts and reports if any step fails; orchestrator can rerun
#      individual agents.

# Then prune:
powershell -File tools/swarm/cleanup.ps1
```

## When to use it

- 3+ parallel agents: always use this system.
- 1-2 parallel agents on truly disjoint files: optional but recommended.
- A single sequential agent: not needed; work directly in the main tree.
