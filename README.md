# Kleiber

Kleiber is an AI-native IDE for Go, written in Go. It treats concurrency, idioms, and runtime data as first-class — not bolted-on extensions.

> **Status:** pre-alpha. Core CLI and backend foundations are in progress. No usable UI yet.
> See [`docs/product/roadmap.md`](docs/product/roadmap.md) for the full milestone plan and
> [`docs/product/vision.md`](docs/product/vision.md) for the product story.

## Development status (2026-05-27)

- [x] Documentation: vision, market analysis, architecture, agent protocol, contributing guides
- [x] Phase 0 — Repo bootstrap: Go module, build scripts, CI, package skeletons, CLI entrypoint
- [ ] **Phase 1 — Core foundations** (in progress): JSON config, logging, typed event bus, app/core composition layer with bootstrap/state snapshots, project model with go.work multi-module package loading/manual refresh/snapshots, command dispatcher, editor/project/LSP command registration, doctor checks
- [ ] Phase 2 — Editor engine (in progress): buffer + undo/redo, view/cursor/selection with external-edit transform, engine-managed buffers/views, and app-owned dispatcher-backed file/buffer/view actions; syntax highlighting pending
- [ ] Phase 3 — LSP client (in progress): gopls subprocess + LSP operations, **editor↔LSP bridge** (didOpen/Change/Close + SaveAs lifecycle + UTF-16-safe diagnostics/navigation routing), completions, buffer formatting, format+save capability for app-level format-on-save, and tracked document snapshot/replay foundation; auto-restart policy pending
- [ ] Phase 4 — UI layer v1 (gioui): pure state/view-model adapter, presenter, typed action/controller, shell boundary, minimal read-only Gio renderer, and first command-palette navigation shell behind the `gio` build tag exist; editor widget/input and palette command execution pending
- [ ] Phase 5 — Debugger & test runner (Delve via DAP, coverage, benchmarks)
- [ ] Phase 6 — AI bridge (providers, gopls MCP, validated refactors)
- [ ] Phase 7 — Runtime awareness (pprof, concurrency visualizer, traces)
- [ ] Phase 8 — Cloud & containers (Docker, k8s, remote dev, OTel)
- [ ] Phase 9 — Public beta (auto-update, telemetry opt-in, signed releases)

This section is updated every 1–2 days.

## Quick start

Requires Go 1.25+ and `make`. The Go floor is set by `go.mod` and ADR-007.
See [`docs/contributing/setup.md`](docs/contributing/setup.md) for the full setup guide and tool prerequisites.

```bash
git clone https://github.com/Ondotteess/kleiber.git
cd kleiber
make build
./bin/kleiber --version
```

`make test` runs unit tests, `make lint` runs the full linter suite, `make coverage` opens an HTML coverage report.

On Windows without `make`, the equivalent local check is:

```powershell
powershell.exe -ExecutionPolicy Bypass -File .\scripts\check.ps1
```

If your PowerShell policy already allows local scripts, `./scripts/check.ps1`
works too.

Experimental UI slice:

```powershell
go run ./cmd/kleiber experimental-ui --smoke [path]
```

This builds the app session, shell, and read-only render model, prints a concise
summary, skips the native window, and does not require `-tags=gio`. It also does
not start `gopls` automatically.

```powershell
go run -tags=gio ./cmd/kleiber experimental-ui [path]
```

This opens a minimal read-only Gio window over the current shell state, with
header, project, buffers, commands, editor-placeholder sections, and bounded
window-level shortcuts for state refresh/quit (`F5`, `Ctrl+R`, `Command+R`,
`Ctrl+Q`, `Command+Q`, `Escape`). `Ctrl+P` / `Command+P` opens a read-only
command-palette shell, Up/Down moves selection with wraparound, Escape closes
the palette before quitting the window, and Enter is intentionally pending for
execution. The default `kleiber` invocation still prints the pre-alpha
notice, and builds without `-tags=gio` reject window mode before opening a
project. The optional `[path]` defaults to the current directory. The editor
widget, file tree interaction, and command execution from the palette are still
pending; human visual smoke is still recommended for the experimental window.
Use the manual runbook in
[`docs/contributing/gio-smoke.md`](docs/contributing/gio-smoke.md) when checking
the native Gio window.

LSP integration tests use a real `gopls`. The normal integration lane skips
cleanly when `gopls` is not on `PATH`; to require real LSP coverage locally:

```powershell
$env:KLEIBER_REQUIRE_GOPLS_INTEGRATION='1'; go test -tags=integration ./internal/lsp
```

## Documentation

Docs are split into two tracks.

### For humans (contributors, early users, stakeholders)

Read these to understand **what** Kleiber is, **why** it exists, and **how** to contribute.

- [`docs/product/vision.md`](docs/product/vision.md) — product vision, target user, positioning
- [`docs/product/market-analysis.md`](docs/product/market-analysis.md) — competitive landscape and the gaps we exploit
- [`docs/product/roadmap.md`](docs/product/roadmap.md) — milestones and timeline
- [`docs/architecture/overview.md`](docs/architecture/overview.md) — high-level system architecture
- [`docs/architecture/components.md`](docs/architecture/components.md) — component breakdown and responsibilities
- [`docs/architecture/decisions.md`](docs/architecture/decisions.md) — Architecture Decision Records (ADRs)
- [`docs/contributing/setup.md`](docs/contributing/setup.md) — local dev environment setup
- [`docs/contributing/workflow.md`](docs/contributing/workflow.md) — git, PRs, reviews, releases
- [`docs/contributing/gio-smoke.md`](docs/contributing/gio-smoke.md) — manual visual smoke for the experimental Gio window
- [`docs/contributing/coding-standards.md`](docs/contributing/coding-standards.md) — Go style guide for this project
- [`docs/glossary.md`](docs/glossary.md) — terminology used across the codebase and docs

### For coding agents (Claude Code, Cursor agents, Aider, etc.)

Read these **first**, before touching any code. They define the protocol every agent must follow.

- [`docs/agents/PROTOCOL.md`](docs/agents/PROTOCOL.md) — **mandatory** rules of engagement for any AI coding agent
- [`docs/agents/codebase-map.md`](docs/agents/codebase-map.md) — where things live in the repo
- [`docs/agents/task-templates.md`](docs/agents/task-templates.md) — how to scope and report tasks
- [`docs/agents/forbidden-actions.md`](docs/agents/forbidden-actions.md) — things agents must never do

## Reading order

**If you're new and human**, read in this order:

1. This file
2. [`docs/product/vision.md`](docs/product/vision.md)
3. [`docs/architecture/overview.md`](docs/architecture/overview.md)
4. [`docs/contributing/setup.md`](docs/contributing/setup.md)

**If you're an AI agent**, read in this order:

1. [`docs/agents/PROTOCOL.md`](docs/agents/PROTOCOL.md) — **before doing anything**
2. [`docs/agents/codebase-map.md`](docs/agents/codebase-map.md)
3. [`docs/agents/forbidden-actions.md`](docs/agents/forbidden-actions.md)
4. The specific docs relevant to your task

## Updating docs

Docs are part of the product. Out-of-date docs are a bug.

- Any PR that changes behavior must update relevant docs in the same PR.
- ADRs (`docs/architecture/decisions.md`) are append-only — never edit a past decision, supersede it. Records in `Proposed` may be finalized to `Accepted` without superseding.
- The "Development status" section above is updated every 1–2 days; everything else is updated as needed.

## License

[MIT](LICENSE). © 2026 Ondotteess and the Kleiber contributors.
