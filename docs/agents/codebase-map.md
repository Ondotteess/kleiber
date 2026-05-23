# Codebase Map

A guide for agents (and new humans) on where things live and where to look.

This map describes the **intended** layout. Some directories may not yet exist at the time of reading; check the actual filesystem.

## Top-level

```
kleiber/
├── cmd/                # Binary entry points
│   └── kleiber/        # Main IDE binary (main package only — no logic here)
├── internal/           # Private application code (not importable outside repo)
│   ├── ai/             # AI Bridge: LLM providers, MCP client, prompt building
│   ├── commands/       # Command Dispatcher
│   ├── config/         # Config loading and persistence
│   ├── editor/         # Editor engine: buffers, views, undo
│   ├── lsp/            # LSP client and gopls supervision
│   ├── project/        # Project model: modules, packages, file watching
│   ├── runtime/        # Runtime monitor: Delve, pprof, future OTel
│   └── ui/             # UI layer (stack TBD per ADR-001)
├── pkg/                # Reusable public packages (sparse, intentional)
├── docs/               # All documentation
├── tests/              # Cross-package integration tests
├── fixtures/           # Fixture Go projects used by tests
├── scripts/            # Dev scripts (build, release, lint)
├── go.mod
├── go.sum
├── Makefile
├── README.md
└── LICENSE
```

## Where to find things

### "I need to add a new command"

1. Define the command in `internal/commands/`.
2. Register it from wherever the command is owned (e.g., a buffer command in `internal/editor/`).
3. Add a test in `internal/commands/<command>_test.go`.
4. Update the command palette docs at `docs/contributing/commands.md` (TBD).

### "I need to handle a new LSP request type"

1. Add the request struct in `internal/lsp/messages.go`.
2. Add the client method in `internal/lsp/client.go`.
3. Add a fixture-based integration test in `internal/lsp/client_integration_test.go` that drives a real `gopls`.

### "I need to add a new AI provider"

1. Implement the `Provider` interface in `internal/ai/providers/<name>.go`.
2. Register it in `internal/ai/providers/registry.go`.
3. Add the provider's config to `internal/config/ai.go`.
4. Add a test using a mocked HTTP server in `internal/ai/providers/<name>_test.go`.

### "I need to change how files are parsed"

Don't. We use `go/parser` and `gopls`. If neither does what you want, escalate to a human before reinventing parsing.

### "I need to expose a new bit of runtime data to the UI"

1. Add the data type in `internal/runtime/types.go`.
2. Source it (Delve, pprof, or other) in the appropriate subpackage.
3. Add an event in `internal/runtime/events.go`.
4. UI subscribes to the event and renders it.

### "I need to add a configuration option"

1. Add the field to the relevant struct in `internal/config/`.
2. Add a sensible default in the same file.
3. Document the option in `docs/contributing/config.md` (TBD).

## Important files

- `go.mod` — Module root and dependency graph. Adding a dependency requires human approval (see PROTOCOL §0).
- `Makefile` — Common dev commands: `make build`, `make test`, `make lint`, `make run`.
- `.golangci.yml` — Linter configuration. Do not weaken without ADR.
- `.github/workflows/*` — CI configuration. Agents may not modify.
- `docs/agents/PROTOCOL.md` — Agent contract. Agents may not modify.

## Things that look like code but aren't

- Files in `/fixtures/` are deliberately broken or unusual Go programs used by tests. Do not "fix" them.
- Files in `/testdata/` (when present under a package) are test inputs. Same rule.
- `/scripts/*.sh` may contain build automation. Read the script before running it.

## Things that don't exist yet

If a directory listed above is missing, it just hasn't been created yet. Agents should not create empty directories preemptively — wait until there's code to put in them.
