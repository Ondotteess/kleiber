package doctor

import "context"

// Check is one diagnostic the Doctor runs against a project root.
//
// Implementations are expected to be self-contained: they receive only
// the absolute root path and a context, and return exactly one Finding
// describing what they observed. A Check that has nothing concerning
// to report returns a Finding with Severity SeverityOK and a brief
// Title.
//
// Checks should respect ctx: if ctx is canceled mid-run, return a
// Finding with whatever can be reported quickly (typically a Title
// like "context canceled"). The Doctor will not call Run after ctx is
// already canceled, so the most common cancellation path is for a
// Check to ignore ctx entirely.
type Check interface {
	// Name returns a stable identifier used for filtering and logging.
	// Conventionally a single lower-case word ("toolchain", "tools",
	// "workspace").
	Name() string

	// Run executes the check. The returned Finding's CheckName is
	// expected to match Name(); Doctor will normalize that if Check
	// forgets, but the convention helps tests.
	Run(ctx context.Context, root string) Finding
}
