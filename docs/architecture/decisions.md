# Architecture Decision Records (ADRs)

ADRs are append-only. To change a past decision, write a new ADR that supersedes the old one. Never edit history.

ADR format:
- **ID** — sequential, never reused.
- **Status** — `Proposed`, `Accepted`, `Superseded by ADR-X`, `Rejected`.
- **Context** — what is happening and why we need to decide.
- **Decision** — the choice made.
- **Consequences** — what becomes easier, what becomes harder.

---

## ADR-001: UI stack — **Accepted** (2026-05-23)

**Context.** Kleiber must render a desktop UI with editor, panels, dialogs. Three viable approaches were considered:

1. **Native GUI in Go** via [`gioui`](https://gioui.org). Pure-Go, single binary, fast startup. Risk: smaller community, less mature widget set, no WebView fallback for HTML content (AI chat would need custom rendering).
2. **WebView + Go backend** via [Wails](https://wails.io) or similar. We write the UI in HTML/CSS/JS and the backend in Go; the binary embeds a system WebView. Familiar UI tooling, easier for AI chat panels and rich content. Risk: binary depends on the system WebView (WebKit on macOS, WebView2 on Windows), inconsistent rendering across platforms.
3. **TUI** via [Bubble Tea](https://github.com/charmbracelet/bubbletea). Easy to start, beloved by terminal users. Risk: hard to show visualizations like a goroutine graph; not a "real IDE" for most target users.

**Decision.** Build the v1 desktop UI on **gioui**.

**Rationale.**
- Aligns with the "single Go binary, no Electron / no JVM" principle from `architecture/overview.md`.
- Pure-Go yields consistent rendering across Linux / macOS / Windows without depending on the system WebView.
- The <500 ms cold-start budget in `product/vision.md` is achievable with gioui; Wails/Electron-class stacks routinely miss it.
- The Concurrency Visualizer (Milestone 4) needs custom drawing; gioui's immediate-mode painting fits naturally.

**Consequences.**
- Steeper ramp for contributors used to HTML/CSS/JS. Mitigated by a small set of internal UI primitives in `internal/ui`.
- AI chat panel will need custom rich-text rendering (no `<div>` shortcut). Acceptable.
- A future TUI mode (Bubble Tea) remains possible as a separate front-end over the same Core API.

---

## ADR-002: Language — **Accepted**

**Context.** Kleiber is an IDE for Go. It could itself be written in Rust, Go, C++, or any other systems language.

**Decision.** Write Kleiber in Go.

**Rationale.**
- Dogfooding: we use the language we are building for.
- Easier to read Go AST and integrate `gopls`/`go/parser`/`go/types` directly.
- The target user community will find the codebase familiar and contribute more easily.
- Performance is sufficient for an IDE.

**Consequences.** UI performance ceiling is lower than Rust + Slint. Memory footprint is higher than Rust. Both tradeoffs are acceptable.

---

## ADR-003: `gopls` as the language brain — **Accepted**

**Context.** We need code intelligence: completions, hovers, references, diagnostics. We can either build it ourselves on top of `go/parser` and `go/types`, or use `gopls` (the official Go language server).

**Decision.** Use `gopls` as the language brain. Run it as a subprocess and talk LSP.

**Rationale.**
- `gopls` is built and maintained by the Go team. It handles edge cases (build tags, generated code, GOOS/GOARCH, modules, workspaces) we don't want to reimplement.
- `gopls` v0.20+ exposes an MCP server, which is exactly what our AI Bridge needs.
- If `gopls` is slow or buggy, our work to improve it benefits the whole ecosystem.

**Consequences.**
- We are coupled to `gopls`'s release cycle.
- We must supervise the subprocess robustly (`gopls` crashes happen).
- We do not own the code intelligence — we are a client. This is the right tradeoff.

---

## ADR-004: Delve via DAP, not custom integration — **Accepted**

**Context.** We need a debugger. Delve is the de facto Go debugger. It can be driven via its own JSON-RPC API or via the Debug Adapter Protocol (DAP).

**Decision.** Talk to Delve via DAP.

**Rationale.**
- DAP is a standard protocol. If we ever support remote debugging or other debuggers, the abstraction is already in place.
- DAP support in Delve is actively maintained.
- Existing DAP client libraries reduce work.

**Consequences.** We accept a small amount of protocol overhead in exchange for portability.

---

## ADR-005: Configuration format — **Accepted** (2026-05-23)

**Context.** User and project settings need a serialization format. Candidates: JSON, YAML, TOML, custom.

**Decision.** **JSON** for both user config (`~/.config/kleiber/config.json`) and project config (`.kleiber/config.json`).

**Rationale.**
- Supported directly by the Go standard library — no third-party dependency required.
- IDE configurations from VS Code, gopls, and most JS/Go tooling already use JSON; users are familiar with the format.
- Strict, unambiguous syntax (unlike YAML's indentation rules).
- Schemas can be tracked alongside the Go types in `internal/config`.

**Consequences.**
- No comments in standard JSON. We will publish an annotated `config.example.json` alongside the documentation to compensate.
- If commentability becomes painful, we can revisit with JSONC or a thin wrapper without breaking on-disk format compatibility (JSONC is a superset).
- One less external dependency than TOML would have brought in.

---

## ADR-006: No plugin system at v1 — **Accepted**

**Context.** Should Kleiber support plugins out of the gate?

**Decision.** No plugin system before public beta.

**Rationale.**
- A plugin API is a permanent commitment. Designing it before we know what the core looks like guarantees pain later.
- We need the core to be excellent first. Plugins distract from that.
- Once the core stabilizes, we'll design a plugin API with hindsight.

**Consequences.** Some users will want to extend Kleiber before we let them. That's fine — they can fork or wait.

---

## ADR-007: Go toolchain floor — **Accepted** (2026-05-23)

**Context.** Phase 1 of the development plan adds `golang.org/x/mod`
(used to parse `go.mod` / `go.work` files) and `golang.org/x/tools/go/packages`
(used to enumerate packages). The latest available `golang.org/x/mod` at
the time of writing (v0.36.0) requires `go 1.25.0` in `go.mod`. Pinning
to an older `golang.org/x/mod` is possible but locks the project to a
parser that lags Go-language features used in test fixtures and gopls
(which itself tracks current Go).

Up to this ADR, `go.mod` declared `go 1.23` and `docs/contributing/setup.md`
stated "Go 1.23 or newer".

**Decision.** Raise the Go toolchain floor to **1.25**.

- `go.mod` declares `go 1.25.0`.
- `.github/workflows/ci.yml` matrix pins `go-version: '1.25'`.
- `.golangci.yml` declares `go: "1.25"`.
- `docs/contributing/setup.md` requires "Go 1.25 or newer".

**Rationale.**
- Eliminates a hand-pin on `golang.org/x/mod` that would otherwise need
  bumping ahead of every gopls/go-toolchain bump.
- Go 1.25 is the current stable release; the user base targeted by
  Kleiber (professional Go developers per
  `docs/product/vision.md`) is overwhelmingly on a current toolchain.
- Keeps Kleiber's parsing and analysis paths in lock-step with what
  `gopls` already requires for new language features.

**Consequences.**
- Contributors on Go 1.23/1.24 must upgrade. Documented in `setup.md`.
- The `toolchain` directive in `go.mod` lets the Go command auto-fetch
  a newer toolchain if the host has an older one, mitigating the upgrade
  burden for one-off contributions.
- Any future bump (1.26+) follows the same ADR rule (see
  `docs/agents/forbidden-actions.md` §10).

---

## ADR template (copy this for new ADRs)

```markdown
## ADR-NNN: <title> — **Proposed / Accepted / Superseded by ADR-X / Rejected**

**Context.** What's happening, what problem we're solving.

**Decision.** The choice.

**Rationale.** Why this choice.

**Consequences.** What this enables, what it forecloses.
```
