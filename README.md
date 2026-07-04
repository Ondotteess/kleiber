# Kleiber

Kleiber is an AI-native IDE for Go, written in Go. It treats concurrency, idioms, and runtime data as first-class — not bolted-on extensions.

> **Status:** MVP. A working Go IDE — file tree, syntax-highlighted editing, live
> gopls diagnostics/completion/hover/go-to-definition, and an embedded terminal —
> runs behind the `gio` build tag via `kleiber edit`.
> See [`docs/product/roadmap.md`](docs/product/roadmap.md) for the full milestone plan and
> [`docs/product/vision.md`](docs/product/vision.md) for the product story.

## Development status (2026-07-04)

- [x] Documentation: vision, market analysis, architecture, agent protocol, contributing guides
- [x] Phase 0 — Repo bootstrap: Go module, build scripts, CI, package skeletons, CLI entrypoint
- [x] **Phase 1 — Core foundations**: JSON config, logging (with file output + `--debug`), typed event bus, app/core composition layer with bootstrap/state snapshots, project model with go.work multi-module loading + a filesystem file-tree, command dispatcher, doctor checks (including a `go`-on-PATH check)
- [x] Phase 2 — Editor engine: buffer + undo/redo, rune-aware view/cursor/selection with external-edit transform, engine-managed buffers/views, in-file search, auto-indent, and Go syntax highlighting via `go/scanner`
- [x] Phase 3 — LSP client: supervised gopls subprocess with **automatic crash restart**, editor↔LSP bridge (didOpen/Change/Save/Close + UTF-16-safe diagnostics/navigation), diagnostics, completion, hover, definition, formatting, and a debug JSON-RPC traffic trace
- [x] Phase 4 — UI layer v1 (gioui): a real IDE window — file tree, editor tabs, syntax highlighting, keyboard editing, find bar, diagnostics + problems panel + status bar, completion/hover/definition popups, and an embedded PTY terminal with `go run/build/test` buttons
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

## Using the IDE

The IDE window is built behind the `gio` build tag. It needs `go` and `gopls`
on `PATH` (`go install golang.org/x/tools/gopls@latest`); run `kleiber doctor`
to check your toolchain.

```powershell
go run -tags=gio ./cmd/kleiber edit [path]
```

`path` is a project directory (opened as the workspace) or a file to open;
it defaults to the current directory. The window shows a file tree, editor
tabs with Go syntax highlighting, a problems panel and status bar, and an
embedded terminal. gopls starts automatically and reports diagnostics as you
type. `--debug` raises logging and records the gopls JSON-RPC traffic;
`--log-file PATH` overrides the log location (default: the user cache dir).

Keyboard shortcuts (use `Cmd` instead of `Ctrl` on macOS):

| Shortcut | Action |
| --- | --- |
| `Ctrl+S` | Save (formats via gopls when `formatOnSave` is enabled) |
| `Ctrl+F` | Find in file |
| `Ctrl+Space` | Completion |
| `F1` | Hover |
| `F12` | Go to definition |
| `Ctrl+M` | Toggle problems panel |
| `Ctrl+J` | Toggle terminal (Run / Build / Test / Mod Tidy buttons) |
| `Ctrl+W` | Close tab |

On Linux the Gio window needs the usual desktop dev libraries (Vulkan/OpenGL,
X11 or Wayland, Wayland/xkbcommon headers); see the
[Gio installation notes](https://gioui.org/doc/install). The rest of the
codebase is OS-independent and cross-compiles cleanly.

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
