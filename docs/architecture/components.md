# Components

This document drills into each core component: responsibilities, public API, dependencies, and current status.

> Note: APIs below are sketches subject to change until Milestone 1 closes.

## Editor Engine

**Package:** `internal/editor`

**Responsibility:** Own text buffers and editing operations.

**Public API (sketch):**
```go
type Buffer interface {
    ID() BufferID
    Path() string
    Text() string
    Insert(pos Position, text string) Edit
    Delete(r Range) Edit
    Undo() (Edit, bool)
    Redo() (Edit, bool)
    Subscribe(ch chan<- Event)
}

type EditorEngine interface {
    Open(path string) (Buffer, error)
    Close(id BufferID) error
    Save(id BufferID) error
    Buffers() []Buffer
}
```

**Dependencies:** Project Model (for file resolution), nothing UI.

**Status:** Not started.

## Project Model

**Package:** `internal/project`

**Responsibility:** Represent the open project, its modules, packages, and files. Watch filesystem for changes.

**Public API (sketch):**
```go
type Project interface {
    Root() string
    Modules() []Module
    Packages() []Package
    FileForPath(path string) (File, error)
    Watch(ctx context.Context) <-chan FSEvent
}

type Module struct {
    Path    string
    Dir     string
    GoMod   string
}

type Package struct {
    ImportPath string
    Dir        string
    Files      []string
}
```

**Dependencies:** Standard library `os`, `path/filepath`, `golang.org/x/tools/go/packages` for loading.

**Status:** Not started.

## Command Dispatcher

**Package:** `internal/commands`

**Responsibility:** Map user input to commands. Provide a command palette. Allow programmatic command invocation (essential for testing and for AI agents).

**Public API (sketch):**
```go
type Command interface {
    Name() string
    Execute(ctx Context) error
}

type Dispatcher interface {
    Register(cmd Command)
    Dispatch(name string, args map[string]any) error
    Palette() []Command
}
```

**Dependencies:** None (lowest-level).

**Status:** Not started.

## LSP Client

**Package:** `internal/lsp`

**Responsibility:** Manage `gopls` subprocess. Implement LSP client. Surface diagnostics, completions, hover, references.

**Public API (sketch):**
```go
type Client interface {
    Start(ctx context.Context) error
    Stop() error

    DidOpen(uri string, text string) error
    DidChange(uri string, edits []TextEdit) error

    Hover(uri string, pos Position) (HoverInfo, error)
    Definition(uri string, pos Position) ([]Location, error)
    References(uri string, pos Position) ([]Location, error)
    Diagnostics(uri string) []Diagnostic
}
```

**Dependencies:** External `gopls` binary. JSON-RPC library (likely `go.lsp.dev/jsonrpc2`).

**Status:** Not started. Top priority for Milestone 1.

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

**Public API:** Depends on chosen stack (see ADR-001).

**Status:** Stack selection in progress.

## Storage / Config

**Package:** `internal/config`

**Responsibility:** Load and persist user and project settings.

**Public API (sketch):**
```go
type Config struct {
    Editor   EditorConfig
    LSP      LSPConfig
    AI       AIConfig
    KeyBinds map[string]string
}

func Load() (*Config, error)
func (c *Config) Save() error
```

**Dependencies:** TOML or JSON parser.

**Status:** Not started.

## Cross-component contracts

- **Events bus.** All long-running components publish events to a typed event bus. Subscribers (especially the UI) react.
- **Context everywhere.** Every public function that may block takes `context.Context` as its first argument.
- **No `panic` across package boundaries.** Errors are values, returned and handled.
- **Logging.** Every component takes a `*slog.Logger` in its constructor. No global loggers.
