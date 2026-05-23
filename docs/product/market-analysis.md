# Market Analysis

## Market size and growth

From the 2025 Go Developer Survey (5,379 respondents):

- 91% of Go developers report satisfaction with Go (65% "very satisfied"); satisfaction has been stable since 2019.
- VS Code: ~37% market share among Go developers.
- GoLand: ~28% market share.
- Vim/Neovim: ~15% (estimated).
- Other (Zed, Sublime, LiteIDE, Emacs): remainder.
- 96% of Go developers deploy to Linux-based systems, including containers.
- Embedded/IoT deployment grew from 2% → 8% year-over-year.

The Go developer population is estimated at 2–3 million globally. Even capturing 1% of that as paying users at $10/month yields ~$2.4–3.6M ARR.

## Competitor breakdown

### GoLand (JetBrains)

**Strengths:**
- Best-in-class refactoring, navigation, and code intelligence.
- Integrated debugger, test runner, coverage, profiler.
- Strong monorepo support.

**Weaknesses:**
- $249/year barrier — many indie devs and small teams skip it.
- Resource-heavy: 2+ GB RAM idle, slow cold start.
- JVM-based — startup time and battery consumption suffer.
- AI features are bolted on, not built around the language.
- Closed ecosystem — extension story exists but is rarely used.

**Trajectory:** Mature product, incremental improvements, AI Assistant added but not deeply Go-aware.

### VS Code + Go Extension

**Strengths:**
- Free, cross-platform, well-known.
- Powered by `gopls` — Go's official language server, very high quality.
- Massive extension ecosystem.
- Copilot integration is the de facto AI experience for many developers.

**Weaknesses:**
- Go is one of many languages — no Go-specific UI or workflows.
- Debugger (Delve) configuration is a frequent pain point.
- Extension instability: autocomplete inconsistencies, occasional `gopls` crashes.
- Electron-based — high memory footprint, slow startup.
- AI (Copilot) does not use language semantics — purely text-based suggestions.

**Trajectory:** Will continue dominating polyglot devs. Unlikely to invest in Go-specific deepening.

### Cursor

**Strengths:**
- Best-in-class AI UX (chat, agent mode, in-editor edits).
- Built on VS Code core, so familiar.

**Weaknesses:**
- $20/month for the AI features.
- Same Go-second-class problem as VS Code.
- AI agent has no concept of `gopls`, build tags, or Go semantics.

**Trajectory:** Growing fast, but horizontal — adds languages, not depth.

### Zed

**Strengths:**
- Native Rust binary — fast startup, low memory.
- Modern UI, good collaborative editing.
- Built-in AI panel.

**Weaknesses:**
- Go support is shallow — basically just an LSP wrapper around `gopls`.
- No Go-specific tooling (no test UI, no concurrency tools, no profiler integration).

**Trajectory:** Strong velocity but unfocused on any single language.

### Vim / Neovim with vim-go or gopls

**Strengths:**
- Loved by power users. Fast, minimal, fully customizable.
- Excellent `gopls` integration.

**Weaknesses:**
- Steep learning curve excludes most developers.
- UI for visualizations (debugger, profiler) is limited to TUI.
- No AI integration that's tightly coupled to the editor.

**Trajectory:** Stable niche, won't grow much.

### LiteIDE

Aging Go-only IDE. Tiny user base. Mentioned for completeness.

## Key pain points (from Go Survey 2025)

Top three developer frustrations:

1. **Idiomatic code patterns** — 33% of respondents struggle with what is "the Go way" for a given problem.
2. **Missing language features** — 28%.
3. **Trustworthy module discovery** — 26%.

Verbatim quote from the survey: *"Accessing the help is painful. `go test --help` didn't work, but tells me to type `go help test` instead… visually parsing through text that looks all the same without much formatting… I just lack time to dig into this rabbit hole."*

**Implication:** Developers want guidance, not just tools. Kleiber should teach idioms inline, not just enforce them via a linter.

## AI tools satisfaction

- 17% of Go developers do not use AI tools for meaningful Go work.
- Of those who do use AI tools, only 55% are satisfied.
- Top complaints: non-functioning code, poor-quality suggestions.

**Implication:** There's a clear opportunity for an AI experience that is actually good at Go.

## Where Kleiber wins

Kleiber attacks specific, defensible gaps:

1. **Go-semantic AI.** Every other tool uses general-purpose LLMs with text context. Kleiber feeds the LLM real type information, dependency graphs, and idiomatic patterns from `gopls` and static analysis. This is hard to copy without rebuilding the architecture.
2. **Concurrency Visualizer.** No competitor has this. It requires deep integration with the Go runtime and Delve. Defensible because most teams won't build it.
3. **Runtime overlay.** Connecting profiler/tracer data to editor in real-time requires native integration with pprof, OpenTelemetry, and (eventually) eBPF. Niche but sticky.

## Risks

- **JetBrains shipping deep AI in GoLand.** Likely, but they have organizational headwind to ship something truly Go-native vs. polyglot. Their AI Assistant is shared across all JetBrains IDEs.
- **Microsoft / GitHub building a Go-tier of Copilot.** Possible but unlikely — Go is not in their top 5 languages by volume.
- **The Go team shipping more in `gopls`.** Actually helpful — we sit on top of `gopls` and benefit from its improvements. `gopls` recently added an MCP server (v0.20.0), which we will use.
- **AI fatigue.** Some developers explicitly reject AI tools. We must keep the non-AI features genuinely excellent on their own.
