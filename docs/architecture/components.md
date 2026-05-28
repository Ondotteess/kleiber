# Components

This document drills into each core component: responsibilities, public API, dependencies, and current status.

> Note: APIs below are sketches subject to change until Milestone 1 closes.

## App/Core Composition

**Package:** `internal/app`

**Responsibility:** Compose core services for UI/keybinding/command-palette callers without importing UI code.

**Public API (sketch):**
```go
type Options struct {
    Config     *config.Config
    Dispatcher *commands.Dispatcher
    Editor     *editor.EditorEngine
    Project    *project.Project
    Formatter  BufferFormatter
}

type Session struct {
    CommandPalette() []CommandDescriptor
    Dispatcher() *commands.Dispatcher
    Dispatch(ctx context.Context, name string, args map[string]any) error
    Editor() *editor.EditorEngine
    Buffers() []editor.BufferRef
    Views(bufferID editor.BufferID) []editor.ViewRef
    Project() *project.Project
    ProjectSnapshot() (project.ProjectSnapshot, bool)
    RegisterCommands() error
    SaveBuffer(ctx context.Context, id editor.BufferID) error
}

func NewDefaultSession(ctx context.Context, opts DefaultSessionOptions) (*Session, error)
```

**Dependencies:** `internal/config`, `internal/commands`, `internal/editor`,
optional `internal/project`, and optional LSP formatting capability.

**Status:** Implemented foundation. `NewSession` provides safe defaults for nil
logger, config, dispatcher, and editor engine; project and formatter remain
optional. `NewDefaultSession` is the bootstrap boundary for future UI entry
points: it loads user config or an explicit config path, builds the logger,
creates core defaults, optionally opens a project root, and registers app-owned
commands once without starting UI or gopls. `Session.RegisterCommands` owns
user-facing editor commands
(`editor.openFile`, `editor.newBuffer`, `editor.closeBuffer`,
`editor.saveBuffer`, `editor.saveAsBuffer`, `editor.newView`,
`editor.closeView`, `editor.moveCursor`, `editor.insertText`,
`editor.backspace`, `editor.deleteSelection`) plus `project.refresh`.
`editor.saveBuffer` now lives here and applies `Config.Editor.FormatOnSave`:
plain save when disabled or when no formatter/tracked Go document exists, and
formatter-backed save when enabled for Go buffers. Formatting errors prevent
disk writes. Read-only state APIs provide defensive snapshots for the command
palette, editor buffers, editor views, config, and optional project metadata.
`Session.Dispatch` is the preferred UI-facing mutation path; raw dispatcher
access remains available for lower-level tests and integration work. Query APIs
remain direct typed calls; no dispatcher result-value contract exists yet.

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
mutations and undo/redo). UI-facing command registration now belongs to
`internal/app`, keeping the editor engine focused on editor state and file I/O.
LSP integration exists through `internal/lsp.Bridge`.
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
atomically reload modules/packages after filesystem changes, surfaces
`go/packages` per-package errors instead of exposing a silently broken package graph,
and watches the project tree
while skipping hidden/vendor-like directories. Still pending: project config
overlay, watcher-driven debounced refresh, and UI-facing file tree state.
`project.refresh` command registration is owned by `internal/app`.

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
snapshots, and `commands.Func` for lightweight registrations.
`internal/app.Session` registers user-facing editor/project mutation commands
without depending on a UI; `internal/lsp` registers LSP-specific format commands.
The dispatcher intentionally remains command-only; state/query reads use
`internal/app.Session` snapshot methods and typed component APIs until a
command-result contract is designed. UI keybindings are not wired yet.

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
`lsp.formatAndSaveBuffer` commands for command-palette/UI callers; app-level
`editor.saveBuffer` consumes the bridge as an optional formatter for
format-on-save. It also exposes `TrackedDocuments()` and
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

**Public API (sketch):**
```go
func BuildState(session *app.Session) (State, error)
func NewPresenter(session *app.Session, opts PresenterOptions) (*Presenter, error)
func NewController(p *Presenter, session *app.Session, opts ControllerOptions) (*Controller, error)
func NewShell(p *Presenter, c *Controller, opts ShellOptions) (*Shell, error)
func BuildGioRenderModel(snapshot ShellState) GioRenderModel
func RunGioWindow(ctx context.Context, shell *Shell, opts GioWindowOptions) error // gio build tag

type State struct {
    Commands []CommandItem
    Buffers  []BufferItem
    Views    []ViewItem
    Project  ProjectState
}

type ShellState struct {
    Title  string
    State  State
    Palette CommandPaletteSnapshot
    Dirty  bool
    Closed bool
}

type CommandPaletteSnapshot struct {
    Open          bool
    SelectedIndex int
    Commands      []CommandItem
}
```

**Status:** State adapter, presenter, typed controller, shell foundation, and a
minimal read-only Gio renderer/window slice are implemented. ADR-001 selects
`gioui`, now present in `go.mod`; the actual Gio window loop is isolated behind
the `gio` build tag so normal core checks remain lightweight. The
`cmd/kleiber` Gio launcher owns the required `gioapp.Main()` call while
`internal/ui` owns only the window event loop and render model, and default
builds reject window mode before opening a project. The
`kleiber experimental-ui --smoke [path]` command is a no-window verification path
that builds the app session, shell, and render model, prints a deterministic
summary, and does not require `-tags=gio` or start `gopls`. Manual native-window verification is tracked in
[`docs/contributing/gio-smoke.md`](../contributing/gio-smoke.md), which defines
the expected visual sections, close behavior, and failure-capture steps. The UI
is still experimental: no editor widget, file tree interaction, palette command
execution, or production input handling exists.
Implemented: `BuildState(session)` converts `internal/app.Session` snapshots
into pure view-model data for commands, buffers, views, modules, packages, and
files using deterministic sorting and defensive slices. `Presenter` owns the
current `State`, can rebuild it on demand via `Refresh(ctx)`, subscribes to
editor events through app-owned accessors, and emits buffered/coalesced update
signals so future UI renderers can decide when to repaint without blocking
editor mutations. `Controller` provides typed action methods such as
`OpenFile`, `NewBuffer`, `SaveBuffer`, `SaveAsBuffer`, `NewView`,
`CloseView`, text-edit actions, and `RefreshProject`; these dispatch through
`app.Session.Dispatch` instead of mutating editor or project state directly.
Project refresh currently has no event source, so the controller explicitly
refreshes presenter state and emits a repaint signal after a successful
`project.refresh`. `Shell` composes `Presenter` and `Controller`, exposes
defensive state snapshots, update signals, explicit refresh, dirty status, and
idempotent close semantics as the handoff point for a future window/render loop.
`BuildGioRenderModel` maps shell snapshots to a testable read-only render model
with header, project, buffers, commands, and editor-placeholder sections plus
clear empty states. The `gio` build provides `RunGioWindow` and a minimal
sectioned layout showing the app title, read-only/pre-alpha status, command
summary, project modules/packages/files, buffers, and "editor widget pending"
markers. It also handles bounded window-level shortcuts: `F5` / `Ctrl+R` /
`Command+R` schedule a coalesced shell state refresh outside the Gio frame/key
event path, `Ctrl+P` / `Command+P` open the command-palette shell, Up/Down move
palette selection with wraparound, Enter is a documented no-op while palette
command execution remains pending, and `Ctrl+Q` / `Command+Q` / `Escape`
request a clean native-window close when the palette is not consuming Escape.
If the palette is open, Escape closes it before it can quit the window. Because
`gioapp.Main()` may block forever on desktop platforms, the cmd-owned Gio
lifecycle maps a completed window loop to a controlled process exit after
reporting any window error or recovered runner panic; `internal/ui` does not own
process exit. Refresh errors are surfaced in the render model instead of being
silently swallowed. The UI package does not expose executable command objects,
perform I/O outside underlying app commands, or cache mutable pointers. The
dispatcher remains command-only and returns only errors; query/state reads still
go through app/session snapshots and the UI read model.

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
