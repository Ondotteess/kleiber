# Product Vision

## One-line pitch

Kleiber is an AI-native IDE for Go that understands the language as deeply as a senior Go engineer — including concurrency, idioms, and runtime behavior.

## The problem

Go developers in 2026 face a forced choice between two IDEs:

- **GoLand** — deep Go support, but paid ($249/year), heavyweight, and slow on large monorepos.
- **VS Code + Go extension** — free and lightweight, but Go is a second-class citizen: debugger setup is painful, autocomplete is inconsistent, and AI integrations don't understand Go semantics.

Neither tool addresses three structural gaps:

1. **AI without Go semantics.** Copilot, Cursor, and similar tools generate code by pattern-matching text. They miss Go-specific issues: races, goroutine leaks, missed `defer`, improper `context` propagation, non-idiomatic error handling.
2. **Concurrency is invisible.** Go's killer feature — goroutines and channels — has no visual representation in any IDE. Developers debug deadlocks and races with `print` statements and trace files opened in separate tools.
3. **Runtime is disconnected from code.** Profiles (pprof), traces (OpenTelemetry), and production metrics live in separate dashboards. You can't see "this function is hot in production" while looking at it.

## The vision

A Go developer opens Kleiber and:

- Sees code with **inline runtime context**: CPU hotness, allocation hot-spots, last error frequency in production.
- Writes a function, and the AI assistant — knowing the actual type graph from `gopls` — suggests an idiomatic implementation and flags concurrency issues before the code is even saved.
- Debugs a deadlock by opening the **Concurrency Visualizer** and seeing the live graph of goroutines blocked on channels.
- Connects to a production pod via `kubectl` and sees a goroutine stack snapshot rendered on top of the source code.

All of this in a native Go binary that starts in under 500ms.

## Target user

**Primary:** Professional Go developers building backend services, infrastructure tools, and distributed systems. They:
- Work in teams of 5–50 engineers.
- Maintain services with real production traffic.
- Care about concurrency, performance, and idiomatic code.
- Have used both VS Code and GoLand and found neither fully satisfying.

**Secondary:** Go-curious developers coming from other languages who want a tool that teaches Go idioms, not just edits text.

**Not the target:** Polyglot developers who write Go occasionally alongside five other languages. They are well-served by VS Code.

## Positioning

|                    | VS Code + Go ext | GoLand        | Cursor        | **Kleiber**         |
|--------------------|------------------|---------------|---------------|---------------------|
| Go-first           | ❌                | ✅             | ❌             | ✅                   |
| AI understands Go  | ❌                | partial       | ❌             | ✅                   |
| Concurrency viz    | ❌                | partial       | ❌             | ✅                   |
| Runtime overlay    | ❌                | ❌             | ❌             | ✅                   |
| Fast startup       | partial          | ❌             | ❌             | ✅                   |
| Price              | free             | $249/yr       | $20/mo        | TBD                 |

## Non-goals

To stay focused, Kleiber explicitly does **not** try to:

- Be a general-purpose editor. We will not add first-class support for JS, Python, Rust, or other languages. Syntax highlighting for adjacent files (YAML, Dockerfile, SQL) is fine; LSP for other languages is not.
- Replace VS Code for polyglot workflows.
- Be a notebook, a database client, or an API testing tool. Plugins may add these later.
- Run in the browser. Kleiber is a native desktop application.

## Success metrics

Twelve months after public beta:

- 10,000+ active weekly users.
- 60%+ of users report it as their primary Go IDE.
- Concurrency Visualizer used at least once per week by 40%+ of users.
- AI features have >70% suggestion acceptance rate (vs. industry average of 25–30% for general-purpose AI tools).
