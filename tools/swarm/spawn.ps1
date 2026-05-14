# Spawn an isolated git worktree for a swarm agent.
#
# Usage:
#   powershell -File tools/swarm/spawn.ps1 -AgentId A1 -Baseline HEAD
#
# Creates .swarm/A1/ checked out at the given baseline commit. The
# agent should be instructed to work only inside that directory.

param(
    [Parameter(Mandatory=$true)]
    [string]$AgentId,

    [string]$Baseline = "HEAD"
)

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$swarmDir = Join-Path $repoRoot ".swarm"
$workDir  = Join-Path $swarmDir $AgentId

if (-not (Test-Path -LiteralPath $swarmDir)) {
    New-Item -ItemType Directory -Path $swarmDir -Force | Out-Null
}

# Suppress PowerShell's noisy treatment of git's stderr.
$prevPref = $ErrorActionPreference
$ErrorActionPreference = 'Continue'

if (Test-Path -LiteralPath $workDir) {
    Write-Host "Worktree $workDir already exists; removing..."
    cmd /c "git -C `"$repoRoot`" worktree remove --force `"$workDir`" 2>NUL" | Out-Null
    if (Test-Path -LiteralPath $workDir) {
        Remove-Item -LiteralPath $workDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}

# Use a unique branch name per agent so the worktree owns its history.
$branch = "swarm/$AgentId"
cmd /c "git -C `"$repoRoot`" branch -D `"$branch`" 2>NUL" | Out-Null

cmd /c "git -C `"$repoRoot`" worktree add -b `"$branch`" `"$workDir`" $Baseline" | Out-Null
if ($LASTEXITCODE -ne 0) {
    $ErrorActionPreference = $prevPref
    throw "worktree add failed (exit $LASTEXITCODE)"
}

$ErrorActionPreference = $prevPref

# Copy the upstream-bin into the worktree if present (gitignored, so
# worktree won't get it from history).
$srcBin = Join-Path $repoRoot ".upstream-bin"
$dstBin = Join-Path $workDir ".upstream-bin"
if ((Test-Path -LiteralPath $srcBin) -and -not (Test-Path -LiteralPath $dstBin)) {
    Copy-Item -LiteralPath $srcBin -Destination $dstBin -Recurse -Force
}

# Same for the bcrunner binary (if built locally).
$srcBcr = Join-Path $repoRoot "tools\luau-bcrunner\bcrunner.exe"
$dstBcr = Join-Path $workDir "tools\luau-bcrunner\bcrunner.exe"
if ((Test-Path -LiteralPath $srcBcr) -and -not (Test-Path -LiteralPath $dstBcr)) {
    Copy-Item -LiteralPath $srcBcr -Destination $dstBcr -Force
}

# Same for .upstream/ (gitignored).
$srcUp = Join-Path $repoRoot ".upstream"
$dstUp = Join-Path $workDir ".upstream"
if ((Test-Path -LiteralPath $srcUp) -and -not (Test-Path -LiteralPath $dstUp)) {
    # Use a directory junction to save space; .upstream/ is read-only
    # reference material so sharing is safe.
    cmd /c "mklink /J `"$dstUp`" `"$srcUp`"" | Out-Null
}

Write-Host "READY: $workDir (branch $branch baseline $Baseline)"
Write-Host "Agent should chdir to: $workDir"
