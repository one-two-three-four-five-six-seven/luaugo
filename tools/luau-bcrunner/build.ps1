# Build the luau-bcrunner harness against the upstream Luau VM static sources.
#
# Output: tools/luau-bcrunner/bcrunner.exe
#
# Requires g++ (mingw) on PATH. The luaugo workspace ships its own gcc
# at C:\Users\user\Documents\mingw64\bin\.

$ErrorActionPreference = "Stop"

$root      = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$here      = $PSScriptRoot
$upstream  = Join-Path $root ".upstream"
$vmSrc     = Join-Path $upstream "VM\src"
$vmInc     = Join-Path $upstream "VM\include"
$commonInc = Join-Path $upstream "Common\include"
$astInc    = Join-Path $upstream "Ast\include"
$out       = Join-Path $here "bcrunner.exe"

if (-not (Test-Path -LiteralPath $vmSrc)) {
    throw "Upstream VM sources missing at $vmSrc; run tools/sync-upstream.ps1 first."
}

$sources = @(Get-ChildItem -LiteralPath $vmSrc -Filter "*.cpp" -File | ForEach-Object { $_.FullName })
$sources += (Join-Path $here "bcrunner.cpp")

$gxx = "g++"
if (-not (Get-Command $gxx -ErrorAction SilentlyContinue)) {
    $gxx = "C:\Users\user\Documents\mingw64\bin\g++.exe"
    if (-not (Test-Path -LiteralPath $gxx)) {
        throw "g++ not found on PATH and no fallback at $gxx"
    }
}

Write-Host "Building luau-bcrunner with $gxx ..."

$compileArgs = @(
    "-std=c++17",
    "-O2",
    "-fno-strict-aliasing",
    "-Wno-deprecated-declarations",
    "-I", $vmInc,
    "-I", $commonInc,
    "-I", $astInc,
    "-o", $out
) + $sources

& $gxx @compileArgs
if ($LASTEXITCODE -ne 0) { throw "g++ build failed with exit code $LASTEXITCODE" }

Write-Host "Built $out"
