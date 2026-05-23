#!/usr/bin/env bash
# check.sh — local pre-commit verification, matching CI.
#
# Runs the same set of checks the CI workflow runs on Linux/macOS:
#   1. go mod verify
#   2. go build ./...
#   3. go vet ./...
#   4. gofmt -s -d .   (must be empty)
#   5. go test ./...
#   6. go test -race ./...   (skipped if cgo is unavailable, with a warning)
#
# Exits non-zero on the first failure. Intended for `make lint` parity
# on systems without GNU make.

set -euo pipefail

cd "$(cd "$(dirname "$0")/.." && pwd)"

step() {
    printf '\n==> %s\n' "$1"
}

have_cgo_toolchain() {
    local cgo cc
    cgo=$(go env CGO_ENABLED)
    [ "$cgo" != "0" ] || return 1
    cc=$(go env CC)
    [ -n "$cc" ] || return 1
    command -v "$cc" >/dev/null 2>&1
}

step "go mod verify"
go mod verify

step "go build ./..."
go build ./...

step "go vet ./..."
go vet ./...

step "gofmt -s -d ."
drift=$(gofmt -s -d .)
if [ -n "$drift" ]; then
    printf 'gofmt drift detected:\n%s\n' "$drift"
    exit 1
fi

step "go test ./..."
go test ./...

step "go test -race ./..."
if have_cgo_toolchain; then
    go test -race ./...
else
    printf 'SKIPPED: -race requires cgo (CGO_ENABLED=1 and a C compiler on PATH).\n'
    printf 'Install a C toolchain (gcc/clang on Linux/macOS, MinGW on Windows) for local race coverage.\n'
    printf 'CI runs -race on every PR, so concurrency bugs are still caught.\n'
fi

printf '\nOK - all checks passed.\n'
