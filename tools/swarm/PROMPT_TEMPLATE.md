# Swarm agent prompt template

Use this template when dispatching a swarm agent via the `task` tool.
Fill in the placeholders marked `<<<...>>>`.

```
ROLE: Swarm agent <<<AGENT_ID>>>.

ISOLATED WORKSPACE: <<<C:\Users\user\Documents\luaugo\.swarm\AGENT_ID>>>
This is YOUR PRIVATE git worktree. The main repository is at
C:\Users\user\Documents\luaugo but you MUST NOT touch it; treat it as
read-only reference if you need to look at upstream/, tests/, etc.

BASELINE: <<<commit hash>>>
This worktree starts at this exact commit. Other agents may be running
in parallel but they are in different worktrees that you cannot see.

OWNED FILES (you MAY modify these, and ONLY these):
  <<<LIST exact relative paths e.g.>>>
  - pkg/vm/lib/string.go
  - pkg/vm/lib/string_test.go
  - pkg/vm/<<<some_helper>>>.go (NEW file ok)

FORBIDDEN (will be rejected by the integrator if you change them):
  - any file ending in contract.go in any package
  - internal/common/**
  - go.mod, go.sum
  - any file outside your OWNED FILES list

READ-ONLY REFERENCE (look but don't modify):
  - .upstream/** (upstream Luau C++ source)
  - tests/conformance/** (fixtures)
  - tools/luau-bcrunner/bcrunner.exe (differential harness, built)
  - internal/upstreamvm/** (helper API)
  - internal/vmlog/** (logger)

OBJECTIVE: <<<one or two sentences>>>

WORKFLOW:
1. Read the failing fixture or symptom.
2. Construct a 5-10 line minimal repro inside your worktree at
   <<<workdir>>>/tools/probe/<<<name>>>.luau.
3. Three-way cross-check (compile via luaugo, run via luaugo VM and
   via internal/upstreamvm) to isolate whether the bug is in our
   compiler or our VM.
4. Use LUAUGO_DEBUG=<<<comma list>>> with internal/vmlog to trace.
5. Apply a surgical fix.

VALIDATION (run these inside your worktree):
- go build ./...                            must be clean
- go vet ./...                              must be clean
- go test ./... -count=1 -timeout 300s     must be green
- TestLuaugoVMSuite completion count must not decrease

RETURN:
- root cause(s),
- exact files+lines modified (output of `git diff --stat HEAD`),
- before/after status of the targeted fixture(s),
- any related bugs noticed but not fixed.

DO NOT COMMIT. Leave changes in the worktree. The orchestrator's
integrator script will pick them up.
```
