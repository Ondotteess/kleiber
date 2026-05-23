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
│   Stack: TBD (see ADR-001).                                    │
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

The exact UI stack is an open question; see [`decisions.md`](decisions.md) ADR-001.

Candidates under consideration:
- Native Go GUI via [`gioui`](https://gioui.org).
- WebView + Go backend (Wails or Tauri-style).
- Terminal UI via Bubble Tea (for an experimental TUI mode).

Whichever stack we choose, the core remains UI-agnostic and exposes a clear command/event API.

### Editor Engine

Owns the in-memory representation of open files: text buffers, cursors, selections, undo history, syntax tree.

Key types (subject to change):
- `Buffer` — text storage (likely rope-based for large files).
- `View` — cursor and selection state over a buffer.
- `SyntaxTree` — incremental parse tree (likely via tree-sitter or Go's `go/parser` for `.go` files).

### Project Model

Knows what a "project" is: a directory tree with one or more Go modules. Watches the filesystem for changes. Resolves files to packages.

Wraps `go list -json` and `go.mod` parsing. Detects multi-module workspaces (`go.work`).

### Command Dispatcher

Translates user input (keyboard, mouse, command palette) into structured commands. Provides keybinding configuration. Logs every command for replay/debugging.

### LSP Client

Manages the `gopls` subprocess. Implements the LSP protocol (JSON-RPC 2.0 over stdio). Translates LSP responses into editor-friendly types.

Supervises `gopls`: restarts on crash, surfaces errors to the user without killing the editor.

### AI Bridge

Talks to LLM providers (Anthropic, OpenAI, Ollama) and the `gopls` MCP server. Builds prompts enriched with Go-semantic context. Validates LLM-proposed code edits before applying.

Provider-agnostic: a `Provider` interface with concrete implementations.

### Runtime Monitor

Connects to Delve for debugging and parses pprof files for profiling. Future: connect to OpenTelemetry collectors and eBPF probes.

Exposes runtime data to the UI as structured events the editor can render inline.

## Data flow examples

### Opening a file

1. User clicks file in tree → UI emits `OpenFileCommand{path}`.
2. Command Dispatcher routes to Project Model.
3. Project Model reads bytes, creates `Buffer`, registers with Editor Engine.
4. Editor Engine notifies UI to render.
5. Editor Engine notifies LSP Client → `textDocument/didOpen` sent to `gopls`.
6. `gopls` returns diagnostics asynchronously → LSP Client delivers them to Editor Engine → UI renders red squiggles.

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

- User preferences: stored in `~/.config/kleiber/config.toml`.
- Project-level settings: `.kleiber/` directory at project root, intended to be checked into git.
- Cache (LSP indexes, AI response cache): `~/.cache/kleiber/`, never committed.
- Logs: `~/.local/state/kleiber/logs/`.

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
