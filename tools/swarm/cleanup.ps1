# Remove all swarm worktrees and prune branches.

param(
    [switch]$Force
)

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$swarmDir = Join-Path $repoRoot ".swarm"

if (-not (Test-Path -LiteralPath $swarmDir)) {
    Write-Host "No .swarm/ directory; nothing to clean."
    exit 0
}

# Remove each worktree.
Get-ChildItem -LiteralPath $swarmDir -Directory | ForEach-Object {
    $work = $_.FullName
    Write-Host "Removing worktree: $work"
    if ($Force) {
        git -C $repoRoot worktree remove --force $work 2>$null | Out-Null
    } else {
        git -C $repoRoot worktree remove $work 2>$null | Out-Null
    }
    if (Test-Path -LiteralPath $work) {
        Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue
    }
}

# Prune swarm branches.
$branches = git -C $repoRoot branch --list "swarm/*"
foreach ($b in $branches) {
    $name = $b.Trim().TrimStart('*').Trim()
    if ($name) {
        git -C $repoRoot branch -D $name 2>$null | Out-Null
    }
}

# Remove empty .swarm/ dir.
if ((Get-ChildItem -LiteralPath $swarmDir -Force | Measure-Object).Count -eq 0) {
    Remove-Item -LiteralPath $swarmDir -Force
}

Write-Host "Swarm cleanup complete."
