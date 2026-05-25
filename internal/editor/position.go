package editor

import "fmt"

// Position is a zero-based location in a text buffer.
//
// Column is the number of bytes between the start of the line and
// the position — not the number of Unicode code points or UTF-16
// code units. Byte columns are chosen because:
//
//   - The underlying buffer is bytes; conversion is free.
//   - Byte offsets round-trip cleanly with ByteOffset/PositionAt.
//   - LSP positions (UTF-16 code units) are translated at the LSP
//     boundary, not inside the editor engine.
//
// Line and Column are independent; the zero value is the start of
// the document.
type Position struct {
	Line   int
	Column int
}

// String renders the position as "L:C" with both axes shifted to
// 1-indexed form, matching how most editors and compilers report
// locations to humans.
func (p Position) String() string {
	return fmt.Sprintf("%d:%d", p.Line+1, p.Column+1)
}

// LessThan reports whether p sorts strictly before other in document
// order.
func (p Position) LessThan(other Position) bool {
	if p.Line != other.Line {
		return p.Line < other.Line
	}
	return p.Column < other.Column
}

// Equal reports structural equality.
func (p Position) Equal(other Position) bool {
	return p.Line == other.Line && p.Column == other.Column
}

// Range is a half-open interval [Start, End) in a buffer. Start is
// inclusive; End is exclusive.
type Range struct {
	Start Position
	End   Position
}

// String renders the range as "Start-End".
func (r Range) String() string {
	return fmt.Sprintf("%s-%s", r.Start, r.End)
}

// Empty reports whether the range covers zero bytes (Start == End).
func (r Range) Empty() bool {
	return r.Start.Equal(r.End)
}

// Normalized returns r with Start <= End. If r is already ordered (or
// empty), it is returned unchanged.
func (r Range) Normalized() Range {
	if r.Start.LessThan(r.End) || r.Start.Equal(r.End) {
		return r
	}
	return Range{Start: r.End, End: r.Start}
}
