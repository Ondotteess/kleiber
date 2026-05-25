package doctor

// Severity classifies a Finding by urgency. Order matters: SeverityOK
// is the lowest, SeverityError is the highest. Callers that aggregate
// Findings to decide an overall exit code typically use the maximum
// severity observed.
type Severity int

// Severity values, ordered low to high. Do not renumber — callers
// compare severities relationally.
const (
	// SeverityOK means the check passed; the Finding is informational.
	SeverityOK Severity = iota

	// SeverityInfo means the situation is mildly unusual but not a
	// problem (e.g., go.mod has no go directive).
	SeverityInfo

	// SeverityWarning means something is likely misconfigured but the
	// project can still build (e.g., dlv missing for debugging,
	// multi-module without go.work).
	SeverityWarning

	// SeverityError means the project is likely broken until fixed
	// (e.g., Go toolchain older than go.mod requires).
	SeverityError
)

// String renders the severity as a lower-case label suitable for logs
// or compact CLI output.
func (s Severity) String() string {
	switch s {
	case SeverityOK:
		return "ok"
	case SeverityInfo:
		return "info"
	case SeverityWarning:
		return "warning"
	case SeverityError:
		return "error"
	default:
		return "unknown"
	}
}

// FixAction describes a concrete remediation step. Label is a short
// imperative name ("Install gopls"). Command is a shell command the
// user can copy-paste to apply the fix. Project Doctor never executes
// commands on behalf of the user; it shows the command for the human
// to run consciously.
type FixAction struct {
	Label   string
	Command string
}

// Finding is one observation made by a Check. CheckName mirrors the
// owning check's Name() so callers can group findings by check; Doctor
// fills this in if the Check omits it.
type Finding struct {
	CheckName string
	Severity  Severity
	Title     string
	Detail    string
	Hint      string
	Fixes     []FixAction
}

// HighestSeverity returns the most severe Severity present in findings,
// or SeverityOK if findings is empty.
func HighestSeverity(findings []Finding) Severity {
	highest := SeverityOK
	for _, f := range findings {
		if f.Severity > highest {
			highest = f.Severity
		}
	}
	return highest
}
