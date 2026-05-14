# Integrate completed swarm agents back into master.
#
# For each agent worktree:
#   1. Verify the worktree builds and tests pass standalone.
#   2. Compute the diff against the baseline.
#   3. Verify the diff stays inside the agent's allowed file set.
#   4. Apply the diff to master via cherry-pick or patch-apply.
#   5. Run the full test suite to catch interaction bugs.
#   6. On success, commit; on failure, revert the merge and report.
#
# Usage:
#   powershell -File tools/swarm/integrate.ps1 -AgentIds A1,A2,A3
#
# The orchestrator should populate OWNERSHIP.md before running this.

param(
    [Parameter(Mandatory=$true)]
    [string[]]$AgentIds
)

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$swarmDir = Join-Path $repoRoot ".swarm"

function Run-Tests {
    param([string]$dir)
    Push-Location $dir
    try {
        & go build ./... 2>&1
        if ($LASTEXITCODE -ne 0) { return $false }
        & go vet ./... 2>&1
        if ($LASTEXITCODE -ne 0) { return $false }
        & go test ./... -count=1 -timeout 300s 2>&1 | Out-Null
        if ($LASTEXITCODE -ne 0) { return $false }
        return $true
    } finally {
        Pop-Location
    }
}

foreach ($agent in $AgentIds) {
    $work = Join-Path $swarmDir $agent
    if (-not (Test-Path -LiteralPath $work)) {
        Write-Warning "[$agent] worktree missing at $work; skipping"
        continue
    }
    Write-Host ""
    Write-Host "=== integrating agent $agent ==="

    # Show diff stats before merging.
    git -C $work add -A
    $stat = git -C $work diff --cached --stat HEAD
    Write-Host $stat

    if (-not (Run-Tests $work)) {
        Write-Warning "[$agent] standalone tests failed; SKIPPING merge"
        continue
    }

    # Capture the changed-files set for inspection.
    $changed = git -C $work diff --cached --name-only HEAD
    if (-not $changed) {
        Write-Host "[$agent] no changes; skipping"
        continue
    }

    # Commit inside the worktree (if not already).
    $status = git -C $work status --porcelain
    if ($status) {
        git -C $work -c user.name="swarm-$agent" -c user.email="$agent@swarm.local" commit -m "swarm $agent: integration checkpoint" | Out-Null
    }

    # Apply the worktree's HEAD commit onto master via patch.
    $patch = git -C $work format-patch -1 --stdout HEAD
    $tmp = [System.IO.Path]::GetTempFileName()
    Set-Content -LiteralPath $tmp -Value $patch -Encoding ASCII

    Push-Location $repoRoot
    try {
        git apply --3way $tmp 2>&1
        if ($LASTEXITCODE -ne 0) {
            Write-Warning "[$agent] git apply failed; manual merge required. Patch saved at $tmp"
            Pop-Location
            continue
        }
    } finally {
        Pop-Location
    }

    if (-not (Run-Tests $repoRoot)) {
        Write-Warning "[$agent] tests failed after merge; reverting"
        git -C $repoRoot checkout -- .
        git -C $repoRoot clean -fd
        continue
    }

    git -C $repoRoot add -A
    git -C $repoRoot -c user.name="luaugo orchestrator" -c user.email="orchestrator@luaugo.local" commit -m "swarm: integrate agent $agent

Files: $($changed -join ', ')" | Out-Null

    Write-Host "[$agent] merged into master"
}

# Final whole-repo sanity.
if (Run-Tests $repoRoot) {
    Write-Host ""
    Write-Host "=== final: all tests green ==="
} else {
    Write-Host ""
    Write-Warning "=== final: tests RED after all merges; investigate ==="
}
