# check.ps1 — local pre-commit verification on Windows, matching CI.
#
# Mirrors scripts/check.sh: go mod verify, build, vet, gofmt check, tests.
# The race detector requires a C toolchain via cgo; if cgo is unavailable
# (typical on a vanilla Windows install without MinGW/MSYS2), the race
# step is skipped with a warning instead of failing the whole check.
# CI runs on GitHub's windows-latest, which ships MinGW, so race still
# runs there.

$ErrorActionPreference = "Stop"

function Step([string]$Name) {
    Write-Host ""
    Write-Host "==> $Name"
}

function HaveCgoToolchain {
    # cgo needs both a C compiler ($env:CC or gcc on PATH) and CGO_ENABLED!=0.
    $cgo = (& go env CGO_ENABLED).Trim()
    if ($cgo -eq "0") { return $false }
    $cc = (& go env CC).Trim()
    if (-not $cc) { return $false }
    return [bool](Get-Command $cc -ErrorAction SilentlyContinue)
}

Push-Location (Join-Path $PSScriptRoot "..")
try {
    Step "go mod verify"
    go mod verify
    if ($LASTEXITCODE -ne 0) { throw "go mod verify failed" }

    Step "go build ./..."
    go build ./...
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }

    Step "go vet ./..."
    go vet ./...
    if ($LASTEXITCODE -ne 0) { throw "go vet failed" }

    Step "gofmt -s -d ."
    $drift = & gofmt -s -d .
    if ($LASTEXITCODE -ne 0) { throw "gofmt failed to run" }
    if ($drift) {
        Write-Host "gofmt drift detected:"
        Write-Host $drift
        throw "gofmt drift"
    }

    Step "go test ./..."
    go test ./...
    if ($LASTEXITCODE -ne 0) { throw "go test failed" }

    Step "go test -race ./..."
    if (HaveCgoToolchain) {
        go test -race ./...
        if ($LASTEXITCODE -ne 0) { throw "go test -race failed" }
    } else {
        Write-Host "SKIPPED: -race requires cgo (CGO_ENABLED=1 and a C compiler on PATH)."
        Write-Host "Install MinGW-w64 or MSYS2 and add gcc to PATH to enable race coverage locally."
        Write-Host "CI runs -race on every PR, so concurrency bugs are still caught."
    }

    Write-Host ""
    Write-Host "OK - all checks passed."
}
finally {
    Pop-Location
}
