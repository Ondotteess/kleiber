# Local Setup

How to get a working Kleiber development environment.

## Prerequisites

- **Go 1.25 or newer.** Check with `go version`. The minimum is driven by `golang.org/x/mod` v0.36+, which Kleiber depends on for `go.mod` / `go.work` parsing.
- **Git.**
- **`make`.** On Windows, use WSL or install GNU Make.
- **`gopls`.** Install with `go install golang.org/x/tools/gopls@latest`.
- **Delve.** Install with `go install github.com/go-delve/delve/cmd/dlv@latest`.

Optional but recommended:
- **`staticcheck`.** `go install honnef.co/go/tools/cmd/staticcheck@latest`.
- **`golangci-lint`.** See [installation guide](https://golangci-lint.run/usage/install/).
- **`goimports`.** `go install golang.org/x/tools/cmd/goimports@latest`.

## Clone

```bash
git clone https://github.com/<org>/kleiber.git
cd kleiber
```

## First build

```bash
go mod download
make build
```

The binary is produced at `./bin/kleiber`.

## Run

```bash
./bin/kleiber
```

> At the current stage of development, the binary may not yet produce a usable UI. Check `docs/product/roadmap.md` for what's actually working today.

## Tests

Run everything:

```bash
make test
```

Run a single package:

```bash
go test -v ./internal/editor/...
```

Run with race detector (slower, but catches concurrency bugs):

```bash
go test -race ./...
```

Run integration tests (separate from unit tests):

```bash
go test -tags=integration ./...
```

The LSP integration suite uses a real `gopls`. By default it skips cleanly when
`gopls` is not on `PATH`; strict verification can require it:

```bash
KLEIBER_REQUIRE_GOPLS_INTEGRATION=1 go test -tags=integration ./internal/lsp
```

PowerShell equivalent:

```powershell
$env:KLEIBER_REQUIRE_GOPLS_INTEGRATION='1'; go test -tags=integration ./internal/lsp
```

## Linters

```bash
make lint
```

Or individually:

```bash
go vet ./...
staticcheck ./...
golangci-lint run
```

For formatting drift, prefer `make lint`, `scripts/check.sh`, or
`scripts/check.ps1`. The scripts check only tracked Go files via
`git ls-files '*.go'` so ignored module/cache directories such as `.gomodcache`
are not part of the repository formatting contract.

## Common dev tasks

- **Format everything before committing**: `make fmt`.
- **Run only tests affected by recent changes**: `go test ./...` is fast enough at this stage; we'll add selective testing later.
- **See test coverage**: `make coverage` (opens HTML report).
- **Update dependencies safely**: `go get <module>@latest` then `go mod tidy`. Note: adding a new dependency requires human approval per [`agents/forbidden-actions.md`](../agents/forbidden-actions.md) §5.

## Editor setup

Yes, the irony is not lost on us: Kleiber developers use VS Code or GoLand until Kleiber can build itself. Both work. We have no project-specific editor configuration yet.

When developing on Kleiber itself, prefer:

- `gopls` with all default checks enabled.
- Format on save with `gofmt`.
- Run `staticcheck` in your editor.

## Troubleshooting

### `gopls` is slow or stuck

- Restart it: in VS Code, "Go: Restart Language Server".
- Check `~/.cache/kleiber/logs/gopls.log` (once Kleiber writes them; until then, check your editor's `gopls` logs).

### Tests fail with "fixture not found"

Make sure `git submodule update --init --recursive` has been run if any fixtures are submodules. (At time of writing, none are.)

### Race detector reports a race

Don't ignore it. File an issue and link the race report.

### `go test -race` fails with "requires cgo" on Windows

The race detector is built on top of TSan, which needs a C compiler. A
vanilla Windows install with Go from the official MSI doesn't ship one,
so `go test -race` errors with `requires cgo; enable cgo by setting
CGO_ENABLED=1`.

To enable race coverage locally on Windows, install a C toolchain:

- **MSYS2** (recommended): https://www.msys2.org — after install, run
  `pacman -S mingw-w64-x86_64-gcc` and add `C:\msys64\mingw64\bin` to
  `PATH`.
- **MinGW-w64 standalone**: https://www.mingw-w64.org

Then `go env CGO_ENABLED=1` (one-time) and re-run:

```powershell
powershell.exe -ExecutionPolicy Bypass -File .\scripts\check.ps1
```

The script auto-detects the toolchain and skips race with a warning if
it's missing — CI on `windows-latest` ships MinGW and runs `-race` on
every PR, so concurrency bugs are still caught upstream.

## Next steps

- Read [`docs/architecture/overview.md`](../architecture/overview.md).
- Read [`docs/contributing/workflow.md`](workflow.md).
- Pick an issue labeled `good first issue`.
