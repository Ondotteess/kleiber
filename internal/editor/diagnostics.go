package editor

// DiagnosticSeverity classifies a Diagnostic by urgency. The constants
// mirror LSP's DiagnosticSeverity values so callers translating from
// LSP-typed structs can map them directly, but the type is editor-
// native — the editor package does not import internal/lsp.
//
// Do not renumber: external callers may persist values.
type DiagnosticSeverity int

// DiagnosticSeverity values.
const (
	// DiagnosticSeverityUnknown is the zero value. Returned when a
	// source omits severity entirely; treat as "informational" for
	// rendering purposes.
	DiagnosticSeverityUnknown DiagnosticSeverity = 0

	// DiagnosticSeverityError is a hard error: code is broken.
	DiagnosticSeverityError DiagnosticSeverity = 1

	// DiagnosticSeverityWarning is something probably wrong but the
	// code still compiles.
	DiagnosticSeverityWarning DiagnosticSeverity = 2

	// DiagnosticSeverityInformation is purely informational.
	DiagnosticSeverityInformation DiagnosticSeverity = 3

	// DiagnosticSeverityHint is the lowest severity — typically a
	// stylistic or refactoring suggestion.
	DiagnosticSeverityHint DiagnosticSeverity = 4
)

// String renders the severity as a lower-case label suitable for logs.
func (s DiagnosticSeverity) String() string {
	switch s {
	case DiagnosticSeverityError:
		return "error"
	case DiagnosticSeverityWarning:
		return "warning"
	case DiagnosticSeverityInformation:
		return "info"
	case DiagnosticSeverityHint:
		return "hint"
	default:
		return "unknown"
	}
}

// Diagnostic is a self-contained description of a problem reported
// for a buffer. It is the editor-native form of LSP's Diagnostic —
// Range is in editor coordinates (byte columns from
// internal/editor.Position), Severity is a small closed enum, and
// Source/Code/Message are plain strings.
//
// Translating layers (currently only the LSP bridge) convert
// upstream protocol types into Diagnostic before publishing.
type Diagnostic struct {
	Range    Range
	Severity DiagnosticSeverity

	// Source identifies who produced the diagnostic, e.g., "compiler",
	// "vet", "staticcheck". Optional.
	Source string

	// Code is the source-specific identifier (e.g., "SA4006"). Optional.
	Code string

	// Message is the human-readable description. Required.
	Message string
}

// BufferDiagnostics is a BufferEvent emitted when an analysis layer
// (currently the LSP bridge) reports a new set of diagnostics for a
// buffer. Each event replaces the previous set in full — subscribers
// should not accumulate across events.
//
// Version, when non-nil, is the document version the diagnostics
// were computed against. It can lag behind the buffer's current
// version under load; subscribers wanting strict consistency should
// reject events whose Version is older than the buffer's current
// Seq() (or equivalent versioning the bridge exposes).
type BufferDiagnostics struct {
	ID          BufferID
	Version     *int
	Diagnostics []Diagnostic
}

func (BufferDiagnostics) bufferEvent() {}
