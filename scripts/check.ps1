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

function TrackedGoFiles {
    # Check only existing tracked files; ignored caches such as .gomodcache may
    # contain dependency testdata, and tracked-but-deleted paths can exist before commit.
    $files = @(git ls-files '*.go' | Where-Object { Test-Path -LiteralPath $_ -PathType Leaf })
    if ($LASTEXITCODE -ne 0) { throw "git ls-files failed" }
    return $files
}

function GofmtDriftFiles([string[]]$Files) {
    $tmpRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("kleiber-gofmt-" + [guid]::NewGuid())
    $utf8NoBom = [System.Text.UTF8Encoding]::new($false)
    $drift = @()
    try {
        foreach ($file in $Files) {
            $src = Join-Path (Get-Location) $file
            $normalized = [System.IO.File]::ReadAllText($src).Replace("`r`n", "`n")

            $tmp = Join-Path $tmpRoot $file
            $tmpDir = Split-Path -Parent $tmp
            New-Item -ItemType Directory -Force -Path $tmpDir | Out-Null
            [System.IO.File]::WriteAllText($tmp, $normalized, $utf8NoBom)

            & gofmt -s -w $tmp
            if ($LASTEXITCODE -ne 0) { throw "gofmt failed to run" }

            $formatted = [System.IO.File]::ReadAllText($tmp).Replace("`r`n", "`n")
            if ($normalized -ne $formatted) {
                $drift += $file
            }
        }
    }
    finally {
        if (Test-Path $tmpRoot) {
            Remove-Item -LiteralPath $tmpRoot -Recurse -Force -ErrorAction SilentlyContinue
        }
    }
    return $drift
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

    Step "gofmt -s -d tracked Go files"
    $goFiles = @(TrackedGoFiles)
    $drift = @(GofmtDriftFiles $goFiles)
    if ($drift.Count -gt 0) {
        Write-Host "gofmt drift detected in:"
        $drift | ForEach-Object { Write-Host $_ }
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
