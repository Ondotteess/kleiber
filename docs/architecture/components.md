# Components

This document drills into each core component: responsibilities, public API, dependencies, and current status.

> Note: APIs below are sketches subject to change until Milestone 1 closes.

## Editor Engine

**Package:** `internal/editor`

**Responsibility:** Own text buffers and editing operations.

**Public API (sketch):**
```go
type Buffer interface {
    Text() string
    Insert(pos Position, text string) (Edit, error)
    Delete(r Range) (Edit, error)
    Undo() (Edit, bool)
    Redo() (Edit, bool)
    Observe(Observer)
}

type EditorEngine interface {
    Open(ctx context.Context, path string) (BufferID, error)
    NewBuffer(text string) BufferID
    Close(id BufferID) error
    Buffer(id BufferID) (*Buffer, error)
    Path(id BufferID) (string, error)
    Dirty(id BufferID) (bool, error)
    Save(ctx context.Context, id BufferID) error
    SaveAs(ctx context.Context, id BufferID, path string) error
    Buffers() []BufferRef
    NewView(bufID BufferID) (ViewID, error)
    View(id ViewID) (*View, error)
    CloseView(id ViewID) error
    Views(bufferID BufferID) []ViewRef
    Events() *events.Topic[BufferEvent]
}
```

**Dependencies:** Project Model (for file resolution), nothing UI.

**Status:** In progress. Implemented: byte-positioned text buffer, insert/delete,
undo/redo, observer callbacks, thread-safe buffer access, engine-managed open/save,
dirty tracking, buffer lifecycle events, and a view/cursor/selection model
(directed `Selection` over a `Buffer`, engine-managed `View` handles with their
own selection events, automatic transform of selections against external buffer
mutations and undo/redo). `internal/editor.RegisterCommands` exposes
dispatcher-backed `editor.openFile`, `editor.newBuffer`, `editor.closeBuffer`,
`editor.saveAsBuffer`, `editor.newView`, `editor.closeView`,
`editor.moveCursor`, `editor.insertText`, `editor.backspace`, and
`editor.deleteSelection` commands for future UI/keybinding callers. LSP
integration exists through `internal/lsp.Bridge`.
Not yet implemented: multi-cursor selections, syntax highlighting, and a
production UI renderer.

## Project Model

**Package:** `internal/project`

**Responsibility:** Represent the open project, its modules, packages, and files. Watch filesystem for changes.

**Public API (sketch):**
```go
type Project interface {
    Root() string
    Snapshot() ProjectSnapshot
    Refresh(ctx context.Context) error
    Modules() []Module
    Packages() []Package
    FileForPath(path string) (File, error)
    Watch(ctx context.Context) (<-chan FSEvent, error)
}

type Module struct {
    Path      string
    Dir       string
    GoMod     string
    GoVersion string
}

type Package struct {
    ImportPath string
    Dir        string
    Files      []string
    TestFiles  []string
}

type File struct {
    Path    string
    Package Package
    IsTest  bool
}

type ProjectSnapshot struct {
    Root     string
    Modules  []Module
    Packages []Package
}
```

**Dependencies:** Standard library `os`, `path/filepath`, `golang.org/x/mod`
for `go.mod` / `go.work` parsing, `golang.org/x/tools/go/packages` for package
loading, and `github.com/fsnotify/fsnotify` for recursive file watching.

**Status:** Implemented foundation. `project.Open` resolves the root, locates
`go.mod` or root-level `go.work`, parses module metadata including Go version,
loads packages through `go/packages` for every module listed in a root `go.work`,
exposes defensive `ProjectSnapshot`/module/package snapshots, resolves files to
owning packages across those modules, supports manual `Refresh(ctx)` to
atomically reload modules/packages after filesystem changes, registers
`project.refresh` for command callers, surfaces `go/packages` per-package errors
instead of exposing a silently broken package graph, and watches the project tree
while skipping hidden/vendor-like directories. Still pending: project config
overlay, watcher-driven debounced refresh, and UI-facing file tree state.

## Command Dispatcher

**Package:** `internal/commands`

**Responsibility:** Map user input to commands. Provide a command palette. Allow programmatic command invocation (essential for testing and for AI agents).

**Public API (sketch):**
```go
type Command interface {
    Name() string
    Description() string
    Execute(ctx context.Context, args map[string]any) error
}

type Dispatcher interface {
    Register(cmd Command) error
    Unregister(name string)
    Dispatch(ctx context.Context, name string, args map[string]any) error
    Palette() []Command
    Has(name string) bool
    Len() int
}
```

**Dependencies:** `log/slog` through injected logger only; otherwise standard library.

**Status:** Implemented foundation. `internal/commands` provides a concurrent-safe
dispatcher, duplicate/empty/nil command validation, sorted command-palette
snapshots, and `commands.Func` for lightweight registrations. Editor, project,
and LSP packages register owned mutation commands without depending on a UI.
The dispatcher intentionally remains command-only; state/query reads use typed
component APIs until a command-result contract is designed. UI keybindings are
not wired yet.

## LSP Client

**Package:** `internal/lsp`

**Responsibility:** Manage `gopls` subprocess. Implement LSP client. Surface diagnostics, completions, hover, references.

**Public API (sketch):**
```go
type Client interface {
    Start(ctx context.Context) error
    Stop(ctx context.Context) error

    DidOpen(ctx context.Context, uri DocumentURI, languageID, text string) error
    DidChange(ctx context.Context, uri DocumentURI, version int, text string) error
    DidClose(ctx context.Context, uri DocumentURI) error

    Hover(ctx context.Context, uri DocumentURI, pos Position) (*Hover, error)
    Completion(ctx context.Context, uri DocumentURI, pos Position) (*CompletionList, error)
    Definition(ctx context.Context, uri DocumentURI, pos Position) ([]Location, error)
    References(ctx context.Context, uri DocumentURI, pos Position, includeDeclaration bool) ([]Location, error)
    Formatting(ctx context.Context, uri DocumentURI, opts FormattingOptions) ([]TextEdit, error)
    Diagnostics() *events.Topic[DiagnosticsEvent]
}
```

**Dependencies:** External `gopls` binary. JSON-RPC/LSP framing is implemented
in-package rather than through a third-party dependency.

**Status:** In progress. Implemented: subprocess supervision, JSON-RPC over stdio,
initialize/initialized handshake, document open/change/close, diagnostics events,
hover, completions, go-to-definition, find references, document formatting requests,
bridge-level buffer formatting via TextEdits, file URI helpers, and
byte-position/UTF-16 conversion in both directions. It also answers
`workspace/configuration` with empty settings
until project/user config is wired in, and acknowledges `window/showMessageRequest`
without selecting an action until UI prompts exist. An editor↔LSP bridge
(`lsp.Bridge`) forwards `editor.BufferOpened/Changed/Closed` to
`textDocument/didOpen|didChange|didClose` for `.go` files with a path, tracks
per-buffer monotonic LSP versions, reconciles SaveAs lifecycle changes by opening
new `.go` paths or closing documents moved out of Go scope, and routes
`publishDiagnostics` back as `editor.BufferDiagnostics` events with UTF-16 ranges
converted to editor byte columns. Versioned diagnostics older than the bridge's
current document version are dropped before conversion, so stale UTF-16 ranges
cannot be mapped against newer editor text. It can also request
editor-position-aware hover, go-to-definition, references, completions, and
formatting for a tracked buffer, translate returned locations/ranges back into
editor byte columns, reject stale navigation, completion, and format results if
the buffer changes mid-request, preserve completion `additionalTextEdits`, apply
returned formatting TextEdits back into the editor buffer, and run an explicit
format-then-save flow. The bridge registers `lsp.formatBuffer` and
`lsp.formatAndSaveBuffer` commands for command-palette/UI callers, plus
`editor.saveBuffer`, which uses `Config.Editor.FormatOnSave` to choose plain
editor save or LSP format-then-save for tracked Go buffers. That save
orchestration currently lives with the LSP bridge until a dedicated app/core
composition layer exists. It also exposes `TrackedDocuments()` and
`ReplayOpenDocuments(ctx)` so restart work has a tested full-text didOpen replay
boundary. Not yet implemented: completion apply UI, code actions, automatic
restart policy, and incremental document sync.

Restart boundary: `Client` is intentionally one-shot today. A gopls crash or
transport close ends the read loop, closes pending requests with
`ErrConnectionClosed`, and leaves restart ownership to a future supervisor. The
bridge can now snapshot tracked documents and replay them as `didOpen` with
current editor text, resetting replayed versions to 1 because LSP didOpen starts
a document version sequence. The next safe implementation step is a supervisor
that creates or attaches a fresh `Client`, reconnects diagnostics/request
ownership, invokes the replay boundary, and only then resumes editor traffic;
hidden auto-restart without that orchestration would desynchronize gopls from
the editor.

## AI Bridge

**Package:** `internal/ai`

**Responsibility:** Talk to LLM providers and `gopls` MCP server. Build Go-aware prompts. Validate AI edits.

**Public API (sketch):**
```go
type Provider interface {
    Name() string
    Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
    Stream(ctx context.Context, req CompletionRequest) (<-chan StreamChunk, error)
}

type Bridge interface {
    Chat(ctx context.Context, prompt string, ctxData ContextBundle) (string, error)
    Refactor(ctx context.Context, selection Range, instruction string) (RefactorProposal, error)
    GenerateTests(ctx context.Context, funcRef FuncRef) (string, error)
}
```

**Dependencies:** LSP Client (for semantic context), HTTP client for providers.

**Status:** Not started. Milestone 3.

## Runtime Monitor

**Package:** `internal/runtime`

**Responsibility:** Connect to Delve for debugging, parse pprof for profiling, eventually OpenTelemetry and eBPF.

**Public API (sketch):**
```go
type Debugger interface {
    Attach(ctx context.Context, pid int) error
    Launch(ctx context.Context, binary string, args []string) error
    SetBreakpoint(file string, line int) (BreakpointID, error)
    Continue() error
    Step() error
    Goroutines() ([]Goroutine, error)
    Eval(expr string) (Value, error)
}

type Profiler interface {
    LoadCPU(path string) (Profile, error)
    LoadHeap(path string) (Profile, error)
    AnnotateBuffer(p Profile, b Buffer) []InlineAnnotation
}
```

**Dependencies:** Delve as subprocess (DAP protocol), `runtime/pprof` decoding.

**Status:** Not started. Milestone 2 (debugger), Milestone 4 (profiler).

## UI Layer

**Package:** `internal/ui` (or `cmd/kleiber-ui` if external)

**Responsibility:** Render the editor and panels. Capture input. Dispatch commands.

**Public API:** Depends on the gioui implementation shape (see ADR-001).

**Status:** Stack accepted, implementation not production-ready. ADR-001 selects
`gioui`, and `internal/ui/doc.go` records the intended contract. No usable editor
window, widgets, command palette, or event rendering exists yet.

## Storage / Config

**Package:** `internal/config`

**Responsibility:** Load and persist user and project settings.

**Public API (sketch):**
```go
type Config struct {
    Editor   EditorConfig
    LSP      LSPConfig
    AI       AIConfig
    Logging  LoggingConfig
    KeyBinds map[string]string
}

func Default() Config
func LoadFile(path string) (Config, error)
func Load() (Config, error)
func SaveFile(path string, cfg Config) error
func Save(cfg Config) error
func UserConfigPath() (string, error)
func UserCachePath() (string, error)
```

**Dependencies:** Standard library JSON parser (`encoding/json`) and filesystem I/O.

**Status:** Implemented foundation. ADR-005 selects JSON. `internal/config` defines
editor, LSP, AI, logging, and keybinding config structs; provides defaults; loads
and saves `config.json` with unknown-field rejection, default filling, platform
user config/cache paths, and atomic writes. `EditorConfig.FormatOnSave` exists
with a conservative default of `false` until UI/settings UX makes the behavior
explicit. Pending: project-level overlay composition and public config
documentation/example files.

## Cross-component contracts

- **Events bus.** `internal/events` implements a generic typed topic with
  subscribe, publish, close, done, and subscriber-count operations.
- **Context everywhere.** Every public function that may block takes `context.Context` as its first argument.
- **No `panic` across package boundaries.** Errors are values, returned and handled.
- **Logging.** Every component takes a `*slog.Logger` in its constructor. No global loggers.
