# PROTOCOL: Rules of Engagement for AI Coding Agents

> **Read this entire document before performing any action on this codebase.**
>
> This document is the contract between the Kleiber project and any AI coding agent (Claude Code, Cursor agents, Aider, custom agents, etc.) operating on this repository. Violations of this protocol are bugs and will result in PR rejection.

Document version: **1.0** (2026-05-23)

---

## 0. Identity and scope

**Who this applies to.** Any non-human or human-supervised agent that reads, writes, or executes code in this repository. Including but not limited to:

- Claude Code (CLI)
- Cursor (Composer / Agent mode)
- Aider
- Devin and similar autonomous agents
- Custom in-house agents

**Who this does not apply to.** Humans authoring code directly, with no agentic assistance for the change. Such authors still follow [`contributing/coding-standards.md`](../contributing/coding-standards.md) and [`contributing/workflow.md`](../contributing/workflow.md).

**Scope of authority.** An agent may:

- Read any file in the repository.
- Propose changes via PR.
- Run tests, linters, formatters, and build commands locally.

An agent may **not**, without explicit human approval in the same chat session:

- Merge a PR.
- Push directly to `main` or any protected branch.
- Modify CI/CD configuration that gates deployment.
- Add a new third-party dependency to `go.mod`.
- Delete more than 50 lines of code in a single change.
- Make changes to any file under `docs/agents/` (this directory).
- Make changes to security-sensitive files: anything matching `**/auth*`, `**/secret*`, `**/credential*`, `**/.github/workflows/*`, or files containing API keys or tokens.

If a task requires any of the above, the agent must stop and request explicit confirmation.

---

## 1. The five mandatory steps

Every task, no matter how small, follows these five steps **in order**:

### Step 1 — Understand the task

Before writing code, the agent must produce a written task understanding that includes:

- **Goal.** One sentence: what does "done" look like?
- **Scope.** Which packages or directories will be touched?
- **Out of scope.** What this task explicitly does *not* change.
- **Acceptance criteria.** Bullet list of observable checks (a specific test passes, a specific command outputs X, a specific file contains Y).

If any of these four are unclear, the agent **must ask the human** before proceeding. Do not guess.

### Step 2 — Explore before editing

Before modifying any file, the agent must:

1. Read [`agents/codebase-map.md`](codebase-map.md) to locate relevant code.
2. Read the existing implementation of any function or type it intends to modify.
3. Read at least one existing test file in the same package to learn the test conventions.
4. Search for existing patterns that solve a similar problem. Prefer extending existing patterns over inventing new ones.

### Step 3 — Plan, then code

Write a brief plan as a comment or chat message before producing code. The plan must list:

- Files to create.
- Files to modify (and what changes in each).
- Tests to add or update.
- Docs to update.

For tasks longer than ~50 lines of code, the plan must be confirmed by the human before code is written.

### Step 4 — Implement with checks

While implementing:

- Run `go build ./...` after every meaningful save. Do not let the tree go unbuildable for long.
- Run `go test ./<package>/...` for each package you change.
- Run `gofmt -s -w .` before declaring the work done.
- Run `go vet ./...` and address any new warnings.
- Run `staticcheck ./...` if it is installed (it is in our CI; install it locally for parity).

### Step 5 — Report

When the task is complete, the agent must produce a report containing:

- **What changed.** Bullet list of files and a sentence per file.
- **Why.** Map each change to an acceptance criterion from Step 1.
- **Tests run.** Names of test files and a one-line summary of results.
- **Open questions.** Anything left unresolved that a human should review.
- **Diffs.** A unified diff or PR link.

Reports are not optional. Without a report, the task is not complete.

---

## 2. Communication conventions

- **Brevity over prose.** Reports and plans are bulleted, not narrative. Save prose for design discussions.
- **No emoji in code or commit messages.** Emoji in chat is fine if the user uses them.
- **No apologies.** "I cannot complete X because Y" is better than "I'm sorry, I can't…". Just state what's blocked and why.
- **Surface uncertainty.** If you're guessing, say so. If you don't know whether an approach is idiomatic Go, say "I'm not sure this is idiomatic — please review".
- **No "I'll do my best".** Either you can do it or you can't. State which.

---

## 3. Code conventions

These are enforced. See [`contributing/coding-standards.md`](../contributing/coding-standards.md) for the full list. The most-violated ones, summarized:

### 3.1 Errors are values

- Always check returned errors. Never `_ = errFunc()` unless paired with a comment explaining why the error is provably ignorable.
- Wrap errors at boundaries: `fmt.Errorf("loading project at %s: %w", path, err)`.
- Use `errors.Is` and `errors.As` for inspection, never string matching.
- Never `panic` in library code. `panic` is acceptable in `main` for unrecoverable startup failures only.

### 3.2 Context is the first parameter

Every function that does I/O, talks to a subprocess, or could block must accept `context.Context` as the first parameter. No exceptions.

### 3.3 No globals

No package-level mutable variables. Loggers, configs, and clients are constructed and passed explicitly. Even `init()` functions are scrutinized — prefer explicit construction.

### 3.4 Test-driven, but pragmatic

- New public functions ship with unit tests in the same PR.
- Test names follow `TestType_Method_Scenario` (e.g., `TestBuffer_Insert_AtEnd`).
- Use table-driven tests for variant inputs.
- Integration tests live in `*_integration_test.go` files with a `//go:build integration` tag.

### 3.5 Concurrency rules

- Goroutines created in a function must terminate before the function returns, or the function must accept and respect a `context.Context` that controls their lifetime.
- Never leak goroutines on error paths.
- Channels: producer closes, consumer does not. If multiple producers, use `sync.WaitGroup` and a single closer goroutine.
- Test concurrent code with `-race`.

### 3.6 Formatting

- `gofmt -s` is the formatter. Anything `gofmt` would change is non-negotiable.
- Imports are grouped: standard library, then external, then internal. `goimports` enforces this.
- Line length: no hard limit, but anything over 120 chars is a code smell worth reconsidering.

---

## 4. Repository conventions

### 4.1 Directory layout

See [`agents/codebase-map.md`](codebase-map.md) for the authoritative map. Summary:

```
/cmd/kleiber/           main entry point
/internal/editor/       editor engine
/internal/project/      project model
/internal/lsp/          LSP client (gopls)
/internal/ai/           AI bridge
/internal/runtime/      Delve, pprof
/internal/commands/     command dispatcher
/internal/ui/           UI layer (stack TBD)
/internal/config/       configuration loading
/docs/                  documentation (this directory)
/tests/                 cross-package integration tests
/fixtures/              test fixture projects
```

`internal/` packages are not importable outside this repo. That's intentional.

### 4.2 Git workflow

- One PR per logical change. No "drive-by" fixes mixed with feature work.
- Branch naming: `agent/<short-task-id>-<slug>` for agent-authored branches.
- Commit messages: imperative present tense ("Add LSP client", not "Added" or "Adds"). Body explains *why*, not *what*.
- Squash before merge.

### 4.3 PR description template

```markdown
## What
<one paragraph>

## Why
<linked issue or design doc; otherwise paragraph>

## How
<bullet list of significant changes>

## Tests
<what tests cover the change>

## Docs
<which docs were updated>

## Generated by
<agent name + version, if applicable>
```

The "Generated by" line is mandatory for agent-authored PRs.

---

## 5. When to stop and ask

Stop and ask the human before proceeding when:

- The task touches files listed in section 0 ("may not, without explicit approval").
- You discover the existing code is materially different from what the task assumed.
- You believe a different approach would be substantially better than the one specified — present both options and let the human choose.
- A test you didn't write starts failing and you don't immediately understand why.
- A new dependency seems necessary.
- You're about to delete or rename a public API.
- You're more than 50% through a task and discover it will take twice as long as estimated.
- The user request conflicts with this protocol — escalate, do not silently violate the protocol.

Stopping early is not a failure. Continuing past these signals is.

---

## 6. Things to never do

A summarized form of [`agents/forbidden-actions.md`](forbidden-actions.md):

- **Never commit secrets, tokens, API keys, or credentials**, even in tests, even commented out.
- **Never disable a test to make CI green.** Either fix it or open an issue.
- **Never silently catch and discard errors.**
- **Never use `interface{}` (or `any`) when a concrete type is known.**
- **Never reach for reflection** unless there's no alternative; document why.
- **Never use `init()` for anything other than registering codecs/decoders.**
- **Never introduce a new package-level mutable variable.**
- **Never rewrite history on shared branches.**
- **Never commit binary artifacts to the repository.**
- **Never edit files in `docs/agents/` without explicit instruction.**

---

## 7. Verification before declaring done

Before the report in Step 5, run this checklist mentally:

- [ ] `go build ./...` succeeds.
- [ ] `go test ./...` succeeds (including `-race` where applicable).
- [ ] `gofmt -s -d .` is empty (no formatting drift).
- [ ] `go vet ./...` is clean.
- [ ] Docs that describe changed behavior are updated.
- [ ] No new TODOs without an associated issue link.
- [ ] No commented-out code.
- [ ] No debug `fmt.Println` or `log.Print` left behind. Use `slog` if logging is intentional.
- [ ] The PR description follows the template in 4.3.
- [ ] The "Generated by" line names the agent.

If any box is unchecked, the work is not done.

---

## 8. How to propose changes to this protocol

This document is part of the protocol it describes. To change it:

1. Open an issue explaining the proposed change and the motivation.
2. Wait for a human maintainer to discuss and accept the change.
3. Submit a PR updating this file and bumping its version number.
4. Until merged and announced, the old version is in effect.

Agents may not unilaterally update this document. That is rule zero.
