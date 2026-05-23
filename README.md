# Kleiber

Kleiber is an AI-native IDE for Go, written in Go. It treats concurrency, idioms, and runtime data as first-class — not bolted-on extensions.

> **Status:** pre-alpha. Phase 0 (repo bootstrap) in progress. No usable UI yet.
> See [`docs/product/roadmap.md`](docs/product/roadmap.md) for the full milestone plan and
> [`docs/product/vision.md`](docs/product/vision.md) for the product story.

## Development status (2026-05-23)

- [x] Documentation: vision, market analysis, architecture, agent protocol, contributing guides
- [ ] **Phase 0 — Repo bootstrap** (in progress): module, build, CI, skeleton packages
- [ ] Phase 1 — Core foundations (config, logging, event bus, project model, command dispatcher)
- [ ] Phase 2 — Editor engine (buffer, view, undo, syntax)
- [ ] Phase 3 — LSP client (gopls subprocess + LSP operations)
- [ ] Phase 4 — UI layer v1 (gioui)
- [ ] Phase 5 — Debugger & test runner (Delve via DAP, coverage, benchmarks)
- [ ] Phase 6 — AI bridge (providers, gopls MCP, validated refactors)
- [ ] Phase 7 — Runtime awareness (pprof, concurrency visualizer, traces)
- [ ] Phase 8 — Cloud & containers (Docker, k8s, remote dev, OTel)
- [ ] Phase 9 — Public beta (auto-update, telemetry opt-in, signed releases)

This section is updated every 1–2 days.

## Quick start

Requires Go 1.23+ and `make`. See [`docs/contributing/setup.md`](docs/contributing/setup.md) for the full setup guide and tool prerequisites.

```bash
git clone https://github.com/Ondotteess/kleiber.git
cd kleiber
make build
./bin/kleiber --version
```

`make test` runs unit tests, `make lint` runs the full linter suite, `make coverage` opens an HTML coverage report.

On Windows without `make`, the equivalent local check is:

```powershell
./scripts/check.ps1
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
