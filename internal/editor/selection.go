package editor

import "fmt"

// Selection is a directed text selection in a Buffer.
//
// Anchor is the fixed end set when the selection began (a click or a
// "begin selection" keystroke). Cursor is the moving end that follows
// the user's input. The two are byte-positioned, matching Position.
//
// A Selection with Anchor == Cursor is a "caret": an empty selection
// that still has a position. Callers that want the textual span of a
// non-empty selection use AsRange, which returns the half-open
// [start, end) interval in document order regardless of selection
// direction.
type Selection struct {
	Anchor Position
	Cursor Position
}

// NewCaret returns a Selection with Anchor and Cursor both at p.
func NewCaret(p Position) Selection {
	return Selection{Anchor: p, Cursor: p}
}

// NewSelection returns a Selection from anchor to cursor preserving
// direction. Use AsRange when direction does not matter.
func NewSelection(anchor, cursor Position) Selection {
	return Selection{Anchor: anchor, Cursor: cursor}
}

// IsEmpty reports whether the selection covers zero bytes
// (Anchor == Cursor). An empty selection is also called a caret.
func (s Selection) IsEmpty() bool {
	return s.Anchor.Equal(s.Cursor)
}

// IsReversed reports whether Cursor is strictly before Anchor in
// document order. Useful for rendering selection direction (e.g. to
// pick which end gets a blinking caret).
func (s Selection) IsReversed() bool {
	return s.Cursor.LessThan(s.Anchor)
}

// AsRange returns the [start, end) interval covered by the selection
// in document order (start <= end), independent of direction.
func (s Selection) AsRange() Range {
	if s.IsReversed() {
		return Range{Start: s.Cursor, End: s.Anchor}
	}
	return Range{Start: s.Anchor, End: s.Cursor}
}

// Collapsed returns a caret at the selection's Cursor end. The
// returned Selection is always empty.
func (s Selection) Collapsed() Selection {
	return NewCaret(s.Cursor)
}

// CollapsedToStart returns a caret at the selection's logical start
// (i.e. min(Anchor, Cursor)). Convenient after a deletion that left
// the caret at the start of the removed range.
func (s Selection) CollapsedToStart() Selection {
	return NewCaret(s.AsRange().Start)
}

// MovedCursor returns a copy of s with Cursor replaced by p. Anchor
// is preserved — i.e. this extends the selection toward p.
func (s Selection) MovedCursor(p Position) Selection {
	return Selection{Anchor: s.Anchor, Cursor: p}
}

// String renders the selection as "Anchor->Cursor".
func (s Selection) String() string {
	return fmt.Sprintf("%s->%s", s.Anchor, s.Cursor)
}

// Equal reports structural equality.
func (s Selection) Equal(other Selection) bool {
	return s.Anchor.Equal(other.Anchor) && s.Cursor.Equal(other.Cursor)
}
