# Synchronize upstream Luau conformance fixtures and regenerate golden bytecode.
#
# Requirements:
#   - git on PATH
#   - The official `luau` binary on PATH (for golden bytecode generation)
#
# Usage:
#   pwsh tools/sync-upstream.ps1
#
# What it does:
#   1. Clones (or updates) a shallow checkout of luau-lang/luau at the
#      tag pinned in tools/UPSTREAM.md into .upstream/
#   2. Copies tests/conformance/*.lua{,u} from the upstream checkout into
#      our tests/conformance/ directory.
#   3. For each fixture, runs `luau --compile=binary` to produce a
#      reference .luac blob into tests/golden/.

param(
    [string]$Tag = "0.720"
)

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
$upstream = Join-Path $repoRoot ".upstream"
$conform  = Join-Path $repoRoot "tests\conformance"
$golden   = Join-Path $repoRoot "tests\golden"

if (-not (Test-Path -LiteralPath $upstream)) {
    git clone --depth 1 --branch $Tag https://github.com/luau-lang/luau.git $upstream
} else {
    Write-Host "Upstream checkout already present at $upstream"
}

# Copy conformance fixtures
$src = Join-Path $upstream "tests\conformance"
Get-ChildItem -LiteralPath $src -Filter "*.lua*" | ForEach-Object {
    Copy-Item -LiteralPath $_.FullName -Destination $conform -Force
}

# Regenerate golden bytecode if `luau` is available.
$luau = Get-Command luau -ErrorAction SilentlyContinue
if ($luau) {
    Get-ChildItem -LiteralPath $conform -Filter "*.lua*" | ForEach-Object {
        $name = [System.IO.Path]::GetFileNameWithoutExtension($_.Name)
        $out  = Join-Path $golden "$name.luac"
        Write-Host "Compiling $($_.Name) -> $out"
        & $luau.Path --compile=binary $_.FullName | Set-Content -LiteralPath $out -Encoding Byte
    }
} else {
    Write-Warning "`luau` binary not on PATH; skipping golden bytecode regeneration."
    Write-Warning "Install upstream Luau and rerun this script before running golden round-trip tests."
}
