# Architecture Overview

This document describes Kleiber's high-level architecture, the major boundaries, and the data flow between them.

## Design principles

1. **Single binary.** Kleiber ships as one statically-linked Go executable. No JVM, no Electron, no npm dependencies at runtime.
2. **Boring core, smart edges.** The core (buffer, files, project model) is conservative and well-tested. Innovation lives in optional layers (AI, runtime overlay).
3. **External tools over reimplementation.** We use `gopls`, Delve, `go test`, `pprof`, and `go tool trace` rather than reimplementing them. Kleiber is an integrator, not a reinventor.
4. **Crash isolation.** Subprocesses (`gopls`, Delve, LLM clients) crash independently of the main editor. The editor must remain responsive even if `gopls` is dead.
5. **No global state.** Every component takes its dependencies explicitly. This makes testing and agent-driven development tractable.

## High-level layout

```
┌───────────────────────────────────────────────────────────────┐
│                        UI Layer                                │
│   Renders panels, accepts input, dispatches commands.          │
│   Stack: gioui (ADR-001); implementation not production-ready. │
└──────────────────────────────┬────────────────────────────────┘
                               │ commands / events
┌──────────────────────────────▼────────────────────────────────┐
│                       Core (Go)                                │
│                                                                │
│  ┌──────────────┐  ┌──────────────┐  ┌────────────────────┐   │
│  │ Editor Engine│  │ Project Model│  │ Command Dispatcher │   │
│  │ (buffer, ops)│  │ (modules,    │  │ (input → action)   │   │
│  │              │  │  files, FS)  │  │                    │   │
│  └──────────────┘  └──────────────┘  └────────────────────┘   │
│                                                                │
│  ┌──────────────┐  ┌──────────────┐  ┌────────────────────┐   │
│  │  LSP Client  │  │  AI Bridge   │  │  Runtime Monitor   │   │
│  │  (to gopls)  │  │  (to LLM)    │  │  (pprof, Delve)    │   │
│  └──────────────┘  └──────────────┘  └────────────────────┘   │
│                                                                │
└──────┬─────────────┬─────────────┬─────────────┬───────────────┘
       │             │             │             │
   ┌───▼───┐    ┌────▼────┐   ┌────▼────┐   ┌────▼────┐
   │ gopls │    │   LLM   │   │  Delve  │   │  pprof  │
   │  LSP  │    │ MCP+API │   │  DAP    │   │  files  │
   └───────┘    └─────────┘   └─────────┘   └─────────┘
```

## Subsystems

### UI Layer

Responsible for rendering panels, capturing input, dispatching commands to the core. The UI does **not** own application state — it observes the core's state and renders it.

The v1 desktop UI stack is **gioui** per [`decisions.md`](decisions.md) ADR-001. As of 2026-05-26, `internal/ui` only contains the package contract; no usable editor UI is implemented yet. The core remains UI-agnostic and exposes a clear command/event API.

### Editor Engine

Owns the in-memory representation of open files: byte-positioned text buffers, views, selections, undo/redo history, dirty tracking, save/save-as, and buffer/view events.

Implemented key types:
- `Buffer` — text storage with insert/delete, undo/redo, observers, byte offsets, and monotonic sequence numbers.
- `EditorEngine` — registry for buffers and views, file open/save, dirty tracking, and typed lifecycle events.
- `View` / `Selection` — cursor and directed selection state over a buffer, including transforms against external edits.

Syntax highlighting and syntax tree ownership are not implemented yet.

### Project Model

Knows what a "project" is: a directory tree with one or more Go modules. Watches the filesystem for changes. Resolves Go files to packages.

Current implementation parses `go.mod` / root-level `go.work` via `golang.org/x/mod`, loads packages through `golang.org/x/tools/go/packages` for every module listed in the workspace, surfaces package-load errors instead of exposing broken package graphs, returns defensive snapshots, resolves files across those modules, supports manual metadata refresh after filesystem changes, and watches the tree recursively through `fsnotify`.

### Command Dispatcher

Translates user input (keyboard, mouse, command palette) into structured commands. The implemented dispatcher is a concurrent-safe registry with `Register`, `Unregister`, `Dispatch`, `Palette`, `Has`, and `Len`; command execution receives `context.Context` and a validated argument map. Editor-owned commands cover basic file, buffer, view, cursor, and text-edit actions; project-owned commands cover explicit metadata refresh; LSP-owned commands cover format, format+save, and config-gated save orchestration. Keybinding configuration is represented in config but not wired to a UI yet. Query/state reads currently remain direct typed API calls such as `EditorEngine.Buffers`, `Project.Snapshot`, and bridge methods until the dispatcher has a result-value contract.

### LSP Client

Manages the `gopls` subprocess. Implements the LSP protocol (JSON-RPC 2.0 over stdio). Translates LSP responses into editor-friendly types.

Current implementation starts/stops `gopls`, performs initialize/initialized, sends didOpen/didChange/didClose, handles version-aware diagnostics, hover, completions, definition, references, formatting, workspace/configuration, and showMessageRequest. `lsp.Bridge` connects `internal/editor` to the client, translates editor byte columns to LSP UTF-16 positions and back, preserves completion `additionalTextEdits`, applies formatting TextEdits, tracks SaveAs lifecycle changes for Go buffers, registers format/format+save plus config-gated save command wiring, and exposes a full-text tracked-document snapshot/replay boundary for future restarts. The client is still one-shot: if the connection closes, pending calls fail and callers must rebuild client+bridge. Automatic restart is deferred until a supervisor can create or attach a fresh client, replay tracked documents, reconnect diagnostics, and resume editor traffic without hiding state loss.

### AI Bridge

Talks to LLM providers (Anthropic, OpenAI, Ollama) and the `gopls` MCP server. Builds prompts enriched with Go-semantic context. Validates LLM-proposed code edits before applying.

Provider-agnostic: a `Provider` interface with concrete implementations.

### Runtime Monitor

Connects to Delve for debugging and parses pprof files for profiling. Future: connect to OpenTelemetry collectors and eBPF probes.

Exposes runtime data to the UI as structured events the editor can render inline.

## Data flow examples

### Opening a file

1. User clicks file in tree → UI dispatches `editor.openFile` with `path`.
2. Command Dispatcher routes to the command owner for file opening.
3. Project Model resolves package/project context for the path when needed.
4. Editor Engine reads bytes, creates `Buffer`, registers it, and publishes `BufferOpened`.
5. UI observes Editor Engine state/events and renders the buffer.
6. LSP Bridge observes `BufferOpened` and sends `textDocument/didOpen` to `gopls`.
7. `gopls` returns diagnostics asynchronously → LSP Client delivers them through the bridge as `editor.BufferDiagnostics` → UI renders red squiggles.

### Saving a Go file

1. UI/keybinding dispatches `editor.saveBuffer`.
2. Command-level orchestration reads `Config.Editor.FormatOnSave`.
3. If disabled, or the buffer is not a tracked Go document, `EditorEngine.Save` writes the file directly.
4. If enabled for a tracked Go document, `lsp.Bridge.FormatAndSaveBuffer` requests formatting from `gopls`, applies TextEdits, then saves. Formatting errors stop before disk write.

Note: `editor.saveBuffer` is currently registered by the LSP bridge package
because format-on-save is the only cross-component save policy. Moving that
orchestration into a future app/core composition layer is tracked as architecture
debt; the editor engine itself remains LSP-agnostic.

### AI-assisted refactor

1. User selects code, presses "Refactor with AI" → UI emits `AIRefactorCommand{range, instruction}`.
2. Command Dispatcher routes to AI Bridge.
3. AI Bridge queries `gopls` MCP server for the selection's type info, references, and package context.
4. AI Bridge builds a prompt with code + semantic context, sends to LLM provider.
5. LLM returns a proposed edit.
6. AI Bridge applies the edit to a copy of the buffer, runs `gopls` diagnostics on the result.
7. If the diff compiles cleanly, UI shows the diff for user approval.
8. On approval, edit is applied to the real buffer.

## Concurrency model

- The main event loop is single-threaded (UI thread).
- Subsystems run in their own goroutines and communicate with the core via channels.
- No shared mutable state across goroutine boundaries — message passing only.
- Long-running operations (LSP requests, AI calls) are always async; the UI never blocks.

## Persistence

- User preferences: stored as JSON in the platform user config directory, e.g. `~/.config/kleiber/config.json` on Linux or `%AppData%\kleiber\config.json` on Windows (ADR-005, `internal/config`).
- Project-level settings: intended path is `.kleiber/config.json` at project root, but project/user overlay composition is not wired yet.
- Cache: platform user cache directory under `kleiber/`, never committed.
- Logs: represented by `internal/config.LoggingConfig`; log file routing is not implemented yet.

## Testing strategy

- Unit tests for every core subsystem.
- Integration tests that spawn real `gopls` against fixture projects.
- End-to-end tests that drive the UI via the command dispatcher (no mouse simulation needed).
- Continuous benchmarks for editor performance on large files (10MB+) and large projects (10k+ files).

## What's deliberately not in scope

- Plugin system (post-beta).
- Built-in package manager (we use `go mod`).
- Built-in version control (we use `git` via shell, no embedded git).
- Database client, REST client, terminal multiplexer (these belong in dedicated tools).
