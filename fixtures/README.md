# Test fixtures

This directory holds small Go projects that integration tests load to
exercise Kleiber against realistic input. Per
[`docs/agents/codebase-map.md`](../docs/agents/codebase-map.md):

> Files in `/fixtures/` are deliberately broken or unusual Go programs
> used by tests. Do not "fix" them.

Each fixture lives in its own subdirectory with a self-explanatory name
(e.g., `hello/`, `multi-module/`, `build-tags/`). Each subdirectory
contains its own `go.mod` so the fixture compiles in isolation — fixtures
are intentionally *not* part of the main Kleiber module.

When adding a fixture:

1. Create a subdirectory named after the scenario it exercises.
2. Include a `README.md` in the fixture describing what the fixture
   demonstrates and which tests rely on it.
3. Ensure the fixture's `go.mod` declares a path under
   `github.com/Ondotteess/kleiber/fixtures/<name>` so editor tooling
   inside the repo behaves predictably.

This directory is intentionally empty until Phase 1 lands and integration
tests need fixtures.
