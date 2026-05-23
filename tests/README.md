# Cross-package integration tests

This directory hosts integration tests that span multiple `internal/*`
packages and cannot live inside a single package's `*_test.go` files
without creating an import cycle.

Files in this directory follow the same conventions as the rest of the
repo (see [`docs/contributing/coding-standards.md`](../docs/contributing/coding-standards.md)):

- File names: `<area>_integration_test.go`.
- Build tag: every file starts with `//go:build integration`.
- Run with `make test-integration` (or `go test -tags=integration ./...`).

Fixture Go projects used by these tests live in [`../fixtures/`](../fixtures/).
Do not put unit tests here — they belong next to the code they exercise.

This directory is intentionally empty until Phase 1 of the development plan
lands; the first integration tests will exercise `internal/project` against
real `go.mod` fixtures.
