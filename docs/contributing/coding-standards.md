# Coding Standards

The Go style guide for this project. We mostly follow [Effective Go](https://go.dev/doc/effective_go) and [Google's Go Style Guide](https://google.github.io/styleguide/go/). The rules below are either reinforcements or project-specific additions.

## Formatting

- `gofmt -s` is law. `goimports` is also law.
- Group imports: standard library, then external, then internal (separated by blank lines).

```go
import (
    "context"
    "fmt"
    "os"

    "github.com/some/external"

    "github.com/<org>/kleiber/internal/editor"
)
```

- No tabs in non-Go files unless required (Makefiles).

## Naming

- Packages: short, lowercase, no underscores, no plurals. `editor`, not `Editors` or `editor_pkg`.
- Files: lowercase with underscores. `lsp_client.go`, not `LspClient.go`.
- Test files: `<source>_test.go` for unit tests, `<source>_integration_test.go` for integration tests.
- Interfaces: name by what they do, not by `-er` suffix when the role isn't a verb. Prefer `Buffer` over `Bufferer`.
- Single-method interfaces: `-er` suffix is fine. `Reader`, `Closer`.
- Acronyms: keep them all the same case. `URL`, `ID`, `LSP` — `userID`, `lspClient`, `parseURL`.
- Receivers: short, consistent within a type. `b *Buffer`, not `buf *Buffer` here and `b *Buffer` there.
- Test names: `TestType_Method_Scenario`. Examples:
  - `TestBuffer_Insert_AtEnd`
  - `TestLSPClient_Hover_OnIdentifier`
  - `TestProject_Open_WithMultiModule`

## Errors

```go
// good
if err != nil {
    return fmt.Errorf("loading project at %s: %w", path, err)
}

// bad — drops context
if err != nil {
    return err
}

// bad — string matching
if strings.Contains(err.Error(), "not found") {
    ...
}

// good — sentinel or typed error
if errors.Is(err, fs.ErrNotExist) {
    ...
}
```

- Never `_ = someFunc()` without a comment justifying why the error is ignorable.
- `panic` only in `main` for unrecoverable startup failures. Otherwise return errors.

## Context

```go
// good — context is first
func (c *Client) Hover(ctx context.Context, uri string, pos Position) (HoverInfo, error)

// bad — context buried
func (c *Client) Hover(uri string, pos Position, ctx context.Context) (HoverInfo, error)
```

- Pass `context.Context` explicitly. Do not store it in structs (except in narrow cases like long-running supervised processes, and only when explicitly documented).
- Every long-running operation respects `ctx.Done()`.
- Never use `context.Background()` deep in the call graph. Accept the caller's context.

## Concurrency

- Every `go` statement must have a clear lifetime story:
  - It exits when the function returns, **or**
  - It respects a passed `ctx`, **or**
  - It listens on an explicit shutdown channel.
- Channels: producer closes, consumer ranges. With multiple producers, use `sync.WaitGroup` and a single closer.
- Use buffered channels only when the buffer size is meaningful and documented. `make(chan T, 100)` with no comment is a code smell.
- Prefer `sync.Mutex` over channels for protecting shared state. Channels are for communication; mutexes are for protecting data.
- Run all concurrent code under `-race` in CI.

## Comments and docs

- Every exported identifier has a doc comment starting with the identifier name.

```go
// Buffer holds the in-memory text of a file being edited.
// A Buffer is safe for concurrent reads but not concurrent writes;
// writers must synchronize externally.
type Buffer interface { ... }
```

- Comments explain *why*, not *what*. The code says what.

```go
// good
// retryLimit is 5 because gopls's startup race condition resolves in <2s
// in practice; 5 with backoff gives us ~10s total.
const retryLimit = 5

// bad
// retryLimit is the limit on retries
const retryLimit = 5
```

- TODOs must include an issue reference: `// TODO(#123): handle multi-module workspaces`.
- No commented-out code. Delete it; `git` remembers.

## Logging

- Use `log/slog` (standard library, Go 1.21+). No `log`, no `fmt.Print` for logging.
- Take a `*slog.Logger` in your constructor; do not use a package-level logger.

```go
func NewClient(logger *slog.Logger) *Client { ... }
```

- Log levels:
  - `slog.LevelDebug`: developer-only detail.
  - `slog.LevelInfo`: meaningful state changes (subprocess started, project opened).
  - `slog.LevelWarn`: something is off but recoverable.
  - `slog.LevelError`: something failed.
- Never log secrets or full file contents at `Info` or above.

## Testing

- Table-driven tests for variant inputs:

```go
func TestBuffer_Insert(t *testing.T) {
    cases := []struct {
        name     string
        initial  string
        pos      Position
        insert   string
        want     string
    }{
        {"empty buffer", "", Position{0, 0}, "hi", "hi"},
        {"at end", "ab", Position{0, 2}, "c", "abc"},
        // ...
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            b := NewBuffer(tc.initial)
            b.Insert(tc.pos, tc.insert)
            if got := b.Text(); got != tc.want {
                t.Errorf("got %q, want %q", got, tc.want)
            }
        })
    }
}
```

- Use `t.Helper()` in test helpers.
- Use `t.TempDir()` for filesystem fixtures — it's auto-cleaned.
- Use `t.Cleanup()` for explicit teardown.
- Integration tests carry a `//go:build integration` tag.

## Generics

- Use generics when they reduce code that is otherwise duplicated for type-only reasons.
- Avoid generics that require complex type constraints to be readable.
- Don't use generics where an interface would do.

## Reflection

- Don't reach for reflection unless there's no alternative. Document the reason if you do.

## Third-party dependencies

- Adding a dependency requires human approval (see [`agents/forbidden-actions.md`](../agents/forbidden-actions.md)).
- Prefer the standard library. If it's "close enough", use it.
- Vendor in `pkg/` if upstream is unmaintained but the code is small.

## File organization

- One concept per file. `buffer.go` defines `Buffer`; `view.go` defines `View`.
- Test files next to source files. No `tests/` subdirectories inside packages.
- Don't create `util.go` or `helpers.go`. Put helpers where they belong.

## What we don't do

- No global mutable state. No package-level vars other than constants and registered codecs.
- No `init()` for business logic. Only registration (e.g., registering a codec with `encoding/gob`).
- No magic numbers. Name them: `const lspRequestTimeout = 5 * time.Second`.
- No abbreviations that aren't already in our [glossary](../glossary.md). `prj` is not `project`, write `project`.
- No `Get` prefix for getters. `buf.Text()`, not `buf.GetText()`.
- No `interface{}` (`any`) when the type is knowable.

## When in doubt

Read existing code in the affected package and match its style. Consistency within a package matters more than personal preference.
