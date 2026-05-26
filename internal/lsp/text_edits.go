package lsp

import (
	"errors"
	"fmt"
	"sort"

	"github.com/Ondotteess/kleiber/internal/editor"
)

var (
	// ErrNilBuffer is returned when ApplyTextEdits receives a nil buffer.
	ErrNilBuffer = errors.New("lsp: nil buffer")

	// ErrInvalidTextEditRange is returned when a TextEdit range is
	// reversed or cannot be applied unambiguously.
	ErrInvalidTextEditRange = errors.New("lsp: invalid text edit range")

	// ErrOverlappingTextEdits is returned when two TextEdits overlap.
	ErrOverlappingTextEdits = errors.New("lsp: overlapping text edits")
)

type byteTextEdit struct {
	index   int
	rng     editor.Range
	newText string
}

// ApplyTextEdits applies LSP TextEdits to buf.
//
// LSP ranges are expressed as UTF-16 positions, while editor.Buffer
// uses byte columns. This helper translates all ranges against the
// original buffer snapshot, rejects overlapping edits, then applies
// edits from the end of the document toward the start so earlier
// coordinates remain valid.
func ApplyTextEdits(buf *editor.Buffer, edits []TextEdit) error {
	if buf == nil {
		return ErrNilBuffer
	}
	if len(edits) == 0 {
		return nil
	}

	text := buf.Text()
	byteEdits := make([]byteTextEdit, len(edits))
	for i, edit := range edits {
		rng, err := RangeToByteRange(text, edit.Range)
		if err != nil {
			return fmt.Errorf("edit %d: %w", i, err)
		}
		if rng.End.LessThan(rng.Start) {
			return fmt.Errorf("%w: edit %d start after end", ErrInvalidTextEditRange, i)
		}
		byteEdits[i] = byteTextEdit{index: i, rng: rng, newText: edit.NewText}
	}

	sort.SliceStable(byteEdits, func(i, j int) bool {
		return byteEdits[i].rng.Start.LessThan(byteEdits[j].rng.Start)
	})
	for i := 1; i < len(byteEdits); i++ {
		prev := byteEdits[i-1]
		cur := byteEdits[i]
		if cur.rng.Start.LessThan(prev.rng.End) || cur.rng.Start.Equal(prev.rng.Start) {
			return fmt.Errorf("%w: edits %d and %d", ErrOverlappingTextEdits, prev.index, cur.index)
		}
	}

	for i := len(byteEdits) - 1; i >= 0; i-- {
		edit := byteEdits[i]
		if !edit.rng.Empty() {
			if _, err := buf.Delete(edit.rng); err != nil {
				return fmt.Errorf("delete edit %d: %w", edit.index, err)
			}
		}
		if edit.newText != "" {
			if _, err := buf.Insert(edit.rng.Start, edit.newText); err != nil {
				return fmt.Errorf("insert edit %d: %w", edit.index, err)
			}
		}
	}
	return nil
}
