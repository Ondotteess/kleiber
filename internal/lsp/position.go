package lsp

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/Ondotteess/kleiber/internal/editor"
)

var (
	// ErrPositionOutOfBounds is returned when a byte-based line/column
	// does not point inside the supplied text.
	ErrPositionOutOfBounds = errors.New("lsp: position out of bounds")

	// ErrInvalidUTF8 is returned when position conversion encounters
	// invalid UTF-8 before reaching the requested position.
	ErrInvalidUTF8 = errors.New("lsp: invalid UTF-8")
)

// PositionFromBytePosition converts a zero-based line plus byte-column
// into an LSP position whose Character is measured in UTF-16 code units.
//
// The editor engine stores columns as byte offsets. LSP (and gopls) use
// UTF-16 code units, so callers must translate at the LSP boundary.
func PositionFromBytePosition(text string, line, column int) (Position, error) {
	if line < 0 || column < 0 {
		return Position{}, fmt.Errorf("%w: line=%d column=%d", ErrPositionOutOfBounds, line, column)
	}

	lineText, ok := lineAt(text, line)
	if !ok {
		return Position{}, fmt.Errorf("%w: line=%d", ErrPositionOutOfBounds, line)
	}

	character, err := utf16UnitsForByteColumn(lineText, column)
	if err != nil {
		return Position{}, err
	}
	return Position{Line: line, Character: character}, nil
}

// RangeFromByteRange converts a zero-based byte range into an LSP range.
func RangeFromByteRange(text string, startLine, startColumn, endLine, endColumn int) (Range, error) {
	start, err := PositionFromBytePosition(text, startLine, startColumn)
	if err != nil {
		return Range{}, fmt.Errorf("start: %w", err)
	}
	end, err := PositionFromBytePosition(text, endLine, endColumn)
	if err != nil {
		return Range{}, fmt.Errorf("end: %w", err)
	}
	return Range{Start: start, End: end}, nil
}

// BytePositionFromPosition converts an LSP UTF-16 position into an
// editor byte-position.
func BytePositionFromPosition(text string, pos Position) (editor.Position, error) {
	if pos.Line < 0 || pos.Character < 0 {
		return editor.Position{}, fmt.Errorf("%w: line=%d character=%d",
			ErrPositionOutOfBounds, pos.Line, pos.Character)
	}

	lineText, ok := lineAt(text, pos.Line)
	if !ok {
		return editor.Position{}, fmt.Errorf("%w: line=%d", ErrPositionOutOfBounds, pos.Line)
	}

	column, err := byteColumnForUTF16Units(lineText, pos.Character)
	if err != nil {
		return editor.Position{}, err
	}
	return editor.Position{Line: pos.Line, Column: column}, nil
}

// RangeToByteRange converts an LSP UTF-16 range into an editor byte range.
func RangeToByteRange(text string, r Range) (editor.Range, error) {
	start, err := BytePositionFromPosition(text, r.Start)
	if err != nil {
		return editor.Range{}, fmt.Errorf("start: %w", err)
	}
	end, err := BytePositionFromPosition(text, r.End)
	if err != nil {
		return editor.Range{}, fmt.Errorf("end: %w", err)
	}
	return editor.Range{Start: start, End: end}, nil
}

func lineAt(text string, want int) (string, bool) {
	start := 0
	line := 0
	for i, r := range text {
		if r != '\n' {
			continue
		}
		if line == want {
			return text[start:i], true
		}
		line++
		start = i + 1
	}
	if line == want {
		return text[start:], true
	}
	return "", false
}

func utf16UnitsForByteColumn(line string, column int) (int, error) {
	if column > len(line) {
		return 0, fmt.Errorf("%w: column=%d line-length=%d", ErrPositionOutOfBounds, column, len(line))
	}
	units := 0
	for offset := 0; offset < column; {
		r, size := utf8.DecodeRuneInString(line[offset:])
		if r == utf8.RuneError && size == 1 {
			return 0, fmt.Errorf("%w: byte offset=%d", ErrInvalidUTF8, offset)
		}
		if offset+size > column {
			return 0, fmt.Errorf("%w: column=%d splits UTF-8 rune", ErrPositionOutOfBounds, column)
		}
		if r > 0xFFFF {
			units += 2
		} else {
			units++
		}
		offset += size
	}
	return units, nil
}

func byteColumnForUTF16Units(line string, character int) (int, error) {
	units := 0
	for offset := 0; offset < len(line); {
		if units == character {
			return offset, nil
		}
		r, size := utf8.DecodeRuneInString(line[offset:])
		if r == utf8.RuneError && size == 1 {
			return 0, fmt.Errorf("%w: byte offset=%d", ErrInvalidUTF8, offset)
		}
		width := 1
		if r > 0xFFFF {
			width = 2
		}
		if units+width > character {
			return 0, fmt.Errorf("%w: character=%d splits UTF-16 surrogate", ErrPositionOutOfBounds, character)
		}
		units += width
		offset += size
	}
	if units == character {
		return len(line), nil
	}
	return 0, fmt.Errorf("%w: character=%d line-units=%d", ErrPositionOutOfBounds, character, units)
}
