package ui

import "unicode/utf8"

// DefaultTabWidth is the fallback tab stop width in cells when a caller
// passes a non-positive width.
const DefaultTabWidth = 4

// VisualColumn returns the display column (in monospace cells, with tabs
// expanded to tabWidth stops) at byteCol within line. line must not
// contain a trailing newline; byteCol is a byte offset assumed to fall on
// a rune boundary and is clamped to the length of line.
//
// It is the inverse of ByteColForVisual for exact hits and underpins
// caret placement: pixelX = VisualColumn(...) * cellWidth.
func VisualColumn(line string, byteCol, tabWidth int) int {
	if tabWidth <= 0 {
		tabWidth = DefaultTabWidth
	}
	if byteCol > len(line) {
		byteCol = len(line)
	}
	col := 0
	for i := 0; i < byteCol; {
		r, size := utf8.DecodeRuneInString(line[i:])
		if size == 0 {
			break
		}
		if r == '\t' {
			col += tabWidth - (col % tabWidth)
		} else {
			col++
		}
		i += size
	}
	return col
}

// ByteColForVisual returns the byte column in line nearest the display
// column visualCol (tabs expanded to tabWidth), snapping to a rune
// boundary and clamping to the line. It maps a mouse-click x position
// (visualCol = round(x / cellWidth)) back to a caret byte offset.
func ByteColForVisual(line string, visualCol, tabWidth int) int {
	if tabWidth <= 0 {
		tabWidth = DefaultTabWidth
	}
	if visualCol <= 0 {
		return 0
	}
	col := 0
	for i := 0; i < len(line); {
		r, size := utf8.DecodeRuneInString(line[i:])
		if size == 0 {
			break
		}
		next := col + 1
		if r == '\t' {
			next = col + tabWidth - (col % tabWidth)
		}
		if visualCol <= col {
			return i
		}
		if visualCol < next {
			// The target falls inside this cell; snap to whichever edge
			// is closer so a click lands on the nearer caret slot.
			if visualCol-col <= next-visualCol {
				return i
			}
			return i + size
		}
		col = next
		i += size
	}
	return len(line)
}

// VisualWidth returns the display width of the whole line in cells.
func VisualWidth(line string, tabWidth int) int {
	return VisualColumn(line, len(line), tabWidth)
}
