// Package doctor diagnoses common setup problems in a Go project and
// suggests concrete fixes.
//
// Each diagnostic is a Check that inspects the project root and returns
// exactly one Finding. The Doctor type orchestrates a list of Checks
// and aggregates their Findings; callers (Kleiber's `kleiber doctor`
// CLI subcommand, an integrated panel later) format and display the
// result.
//
// Project Doctor is intentionally read-only: it never modifies the
// project, never installs tools, never runs `go mod tidy` or similar
// — it tells the user what to do and shows the exact command, but the
// user runs it.
package doctor
