# Roadmap

This roadmap is a living document. Dates are best-effort estimates, not commitments.

Last updated: **2026-05-27**

## Guiding principles

- **Ship vertical slices, not features.** Each milestone delivers something a real user can use end-to-end, even if narrow.
- **No demos that hide broken foundations.** Every milestone passes its own test suite and runs on a developer machine without manual setup.
- **Documentation is part of the milestone.** A feature is not "done" until docs are updated.

## Milestone 1 — Editor MVP (target: Q3 2026)

**Goal:** A working text editor that handles Go files better than `nano`, opens a real Go project, and integrates `gopls`.

Deliverables:
- Native window with text buffer, cursor, selection, basic editing.
- Syntax highlighting for Go.
- File tree and file open/save.
- `gopls` running as child process with LSP message exchange.
- Hover, go-to-definition, find references.
- Save-time formatting through `gopls`/Go formatting semantics.

Out of scope: debugger, tests, AI, multi-window, themes beyond default.

Current implementation checkpoint:
- Runnable CLI surface: `kleiber --version`, `kleiber help`, and `kleiber doctor [path]`.
- Core foundations exist for JSON config, logging, typed events, command dispatch,
  go.work-aware project/module/package loading with manual refresh, filesystem watching,
  defensive project snapshots, an app/core composition layer with bootstrap,
  read-only state snapshots, user-facing editor/project command registration,
  editor buffers/views, a pure UI state/view-model adapter, presenter boundary,
  typed UI action/controller boundary, UI shell boundary, and a minimal
  read-only Gio renderer behind the `gio` build tag, config-gated
  save/format-on-save orchestration, and LSP bridge operations
  including tracked-document snapshot/replay groundwork for a future restart supervisor.
- UI is still not usable as an editor; `internal/ui` contains pure
  state/presenter/controller/shell foundations and an experimental read-only Gio
  window path, but no editor widget, file tree interaction, command palette
  interaction, or production input handling.

## Milestone 2 — Debugger and Test Runner (target: Q4 2026)

**Goal:** A developer can debug and test a Go service from inside Kleiber without touching the terminal.

Deliverables:
- Delve integration: breakpoints, step in/over/out, watch, evaluate.
- Test runner UI: list, run, debug individual tests and subtests.
- Coverage overlay in the editor (lines hit / not hit).
- Benchmark runner with simple time/allocs results table.
- Output panel for `go build`, `go test`, `go run`.

Out of scope: remote debugging, container/k8s targets, fuzzing UI.

## Milestone 3 — AI Layer (target: Q1 2027)

**Goal:** First version of the Go-semantic AI features that differentiate Kleiber from Cursor and Copilot.

Deliverables:
- LLM integration via configurable provider (Anthropic, OpenAI, local Ollama).
- AI chat panel that reads from `gopls` MCP server.
- Inline suggestions enriched with type and package info.
- Go-aware diagnostics: race detection hints, goroutine leak patterns, `context` propagation warnings.
- Table-driven test generation command.
- Refactor proposals validated against the compiler before being applied.

Out of scope: agentic multi-step refactors, codebase-wide AI search, voice input.

## Milestone 4 — Runtime Awareness (target: Q2 2027)

**Goal:** Bring runtime data into the editor.

Deliverables:
- pprof viewer integrated as an editor panel (CPU, heap, allocs, blocks, mutex).
- Inline annotations showing hot functions from a profile.
- Concurrency Visualizer v1: live graph of goroutines and channels from a running process.
- Trace file viewer (`go tool trace` replacement, embedded).
- Snapshot mode: load a `.pprof` or trace file from production and explore it inline with code.

Out of scope: live production attach, OpenTelemetry, eBPF.

## Milestone 5 — Cloud and Containers (target: Q3 2027)

**Goal:** Make Kleiber the best IDE for cloud-native Go.

Deliverables:
- Docker integration: build, run, attach debugger to containerized service.
- Kubernetes integration: list pods, port-forward, attach debugger via `kubectl debug`.
- Remote development: edit and run code on a remote host or container via SSH.
- OpenTelemetry trace ingestion: render distributed traces with click-to-source.

## Milestone 6 — Public Beta (target: Q4 2027)

**Goal:** Open Kleiber to the public.

Deliverables:
- Auto-update mechanism.
- Crash reporting (opt-in).
- Telemetry (opt-in).
- Documentation site at `kleiber.dev`.
- First 100 paying customers (if commercial license is chosen).

## Beyond beta — under consideration

These ideas are parked until after public beta:

- Plugin API for community extensions.
- Collaborative editing (multiplayer mode).
- Built-in `staticcheck`, `govulncheck`, `golangci-lint` integration with explanations.
- Visual schema editor for Protocol Buffers and gRPC services.
- eBPF-based production introspection.
- Mobile companion for monitoring builds and tests.
- Self-hosted "Team" edition with shared AI cache and audit logging.

## How to influence the roadmap

- Open an issue with the `roadmap` label.
- For large changes, propose an ADR in `architecture/decisions.md`.
- Vote on existing roadmap issues — items with the most reactions get prioritized.
