package editor

// ChangeKind classifies what produced a Buffer change.
type ChangeKind int

// ChangeKind values. Do not renumber — callers compare and persist.
const (
	// ChangeKindInsert tags a Change emitted by Buffer.Insert.
	ChangeKindInsert ChangeKind = iota + 1

	// ChangeKindDelete tags a Change emitted by Buffer.Delete.
	ChangeKindDelete

	// ChangeKindUndo tags a Change emitted by Buffer.Undo. The
	// embedded Edit is the original (forward) edit that was just
	// reversed.
	ChangeKindUndo

	// ChangeKindRedo tags a Change emitted by Buffer.Redo. The
	// embedded Edit is the edit that was just reapplied.
	ChangeKindRedo
)

// String renders the kind as a lower-case label suitable for logs.
func (k ChangeKind) String() string {
	switch k {
	case ChangeKindInsert:
		return "insert"
	case ChangeKindDelete:
		return "delete"
	case ChangeKindUndo:
		return "undo"
	case ChangeKindRedo:
		return "redo"
	default:
		return "unknown"
	}
}

// Change describes a single completed mutation of a Buffer. It is
// the value delivered to observers registered via Buffer.Observe.
type Change struct {
	Kind ChangeKind
	Edit Edit

	// Seq is the buffer's Seq() AFTER the change was applied.
	Seq int
}

// Observer is a callback invoked from the goroutine that performed
// a mutation. Observers MUST NOT block: long-running consumers
// should forward the Change to a buffered channel and process it
// in a separate goroutine. Observers are called in registration
// order; an observer registered later runs after one registered
// earlier.
//
// The Buffer holds the observer for its lifetime — there is no
// "Unobserve" in v1. Construct a fresh Buffer if observer churn
// matters.
type Observer func(Change)
