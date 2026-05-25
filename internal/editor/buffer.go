package editor

import (
	"errors"
	"fmt"
	"sync"
)

// Sentinel errors. Callers compare with errors.Is.
var (
	// ErrLineOutOfBounds is returned when a Position's Line is
	// negative or past the buffer's last line.
	ErrLineOutOfBounds = errors.New("editor: line out of bounds")

	// ErrColumnOutOfBounds is returned when a Position's Column is
	// negative or past the end of the line it points into. A Column
	// equal to the line's length is allowed — it denotes the
	// insertion point immediately after the line's last byte.
	ErrColumnOutOfBounds = errors.New("editor: column out of bounds")
)

// Edit describes a single applied change to a Buffer. It carries
// enough state to be reversed: the affected range before the edit,
// the text that was removed (empty for a pure insertion), and the
// text that was inserted (empty for a pure deletion).
//
// Buffers push Edits onto an internal undo stack as side effects of
// Insert and Delete; callers can also inspect Edits returned from
// those methods to implement application-level change tracking.
type Edit struct {
	// Range is the range affected in the buffer BEFORE the edit
	// was applied. For an Insert at P, Range = {P, P}. For a Delete
	// over R, Range = R.
	Range Range

	// Removed is the text that was removed by the edit.
	Removed string

	// Inserted is the text that was inserted by the edit.
	Inserted string
}

// IsEmpty reports whether the edit changes anything.
func (e Edit) IsEmpty() bool {
	return e.Removed == "" && e.Inserted == ""
}

// Buffer is an in-memory text buffer with undo/redo.
//
// The current implementation stores text as a single contiguous
// []byte. Insert and Delete are O(N) in buffer size. This is good
// enough for files up to roughly a megabyte; per
// docs/architecture/components.md the storage is "likely rope-based
// for large files" and will be swapped to a logarithmic structure
// when profiling demands it. The Buffer API does not assume linear
// storage so the change will be source-compatible.
//
// A Buffer is safe for concurrent use. Mutating operations are
// serialized, readers observe a consistent snapshot, and observers are
// invoked after the internal lock has been released.
type Buffer struct {
	mu sync.RWMutex

	data       []byte
	lineStarts []int // byte offset of each line's first byte; always [0, ...]

	undoStack []Edit
	redoStack []Edit

	seq int

	observers []Observer
}

// Observe registers an observer that is called from the mutating
// goroutine after every successful Insert/Delete/Undo/Redo. No-op
// mutations (empty inserts, empty-range deletes) do NOT notify.
//
// A nil observer is silently ignored. Observers must not block —
// see the Observer doc-comment.
func (b *Buffer) Observe(o Observer) {
	if o == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.observers = append(b.observers, o)
}

// notify fans out ch to observers copied while the mutation lock was
// held. It intentionally runs without holding b.mu so observers may
// inspect the buffer.
func notify(observers []Observer, ch Change) {
	for _, o := range observers {
		o(ch)
	}
}

// NewBuffer constructs a Buffer initialized to text. The text is
// copied; callers may mutate the underlying string-backed memory
// afterward without affecting the Buffer.
func NewBuffer(text string) *Buffer {
	b := &Buffer{
		data: []byte(text),
	}
	b.rebuildLineStarts()
	return b
}

// Text returns a copy of the buffer's contents as a string. The
// copy is unavoidable because Go strings are immutable while our
// storage is a mutable []byte.
func (b *Buffer) Text() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return string(b.data)
}

// Length returns the number of bytes in the buffer.
func (b *Buffer) Length() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.data)
}

// Lines returns the number of logical lines in the buffer.
//
// The count includes a possibly-empty trailing line after a final
// newline. Examples:
//
//	""           → 1 line (empty)
//	"foo"        → 1 line ("foo")
//	"foo\n"      → 2 lines ("foo", "")
//	"foo\nbar"   → 2 lines ("foo", "bar")
//	"foo\nbar\n" → 3 lines ("foo", "bar", "")
func (b *Buffer) Lines() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.lineStarts)
}

// LineText returns the text of line (zero-indexed) without its
// trailing newline. The ok return is false when line is out of range.
func (b *Buffer) LineText(line int) (string, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if line < 0 || line >= len(b.lineStarts) {
		return "", false
	}
	start := b.lineStarts[line]
	end := b.lineEndLocked(line)
	return string(b.data[start:end]), true
}

// Seq returns a monotonic counter that increases by one on every
// successful mutation (Insert, Delete, Undo, Redo). Subscribers
// can use it to detect "has the buffer changed since checkpoint X?"
// without holding onto a full snapshot. Empty-edit no-ops do NOT
// bump Seq.
func (b *Buffer) Seq() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.seq
}

// ByteOffset converts a Position to a raw byte offset into the
// buffer's data.
//
// Errors:
//
//   - ErrLineOutOfBounds if pos.Line is negative or past the last line.
//   - ErrColumnOutOfBounds if pos.Column is negative or past the
//     end of the line (Column == lineLength is allowed and points
//     at the position just after the line's last byte).
func (b *Buffer) ByteOffset(pos Position) (int, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.byteOffsetLocked(pos)
}

func (b *Buffer) byteOffsetLocked(pos Position) (int, error) {
	if pos.Line < 0 || pos.Line >= len(b.lineStarts) {
		return 0, fmt.Errorf("%w: line=%d, lines=%d",
			ErrLineOutOfBounds, pos.Line, len(b.lineStarts))
	}
	lineStart := b.lineStarts[pos.Line]
	lineEnd := b.lineEndLocked(pos.Line)
	if pos.Column < 0 || pos.Column > lineEnd-lineStart {
		return 0, fmt.Errorf("%w: column=%d, line-length=%d",
			ErrColumnOutOfBounds, pos.Column, lineEnd-lineStart)
	}
	return lineStart + pos.Column, nil
}

// PositionAt converts a byte offset to a Position. offset must be
// in [0, Length]; an offset equal to Length is the end-of-document.
func (b *Buffer) PositionAt(offset int) (Position, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if offset < 0 || offset > len(b.data) {
		return Position{}, fmt.Errorf("%w: offset=%d, length=%d",
			ErrColumnOutOfBounds, offset, len(b.data))
	}
	// Binary search for the largest line index whose start <= offset.
	low, high := 0, len(b.lineStarts)
	line := 0
	for low < high {
		mid := (low + high) / 2
		if b.lineStarts[mid] <= offset {
			line = mid
			low = mid + 1
		} else {
			high = mid
		}
	}
	return Position{Line: line, Column: offset - b.lineStarts[line]}, nil
}

// Insert inserts text at pos. It returns the resulting Edit (with
// Range.Start == Range.End == pos and Inserted == text) on success.
// Inserting an empty string is a successful no-op: it returns an
// empty Edit, does not push to the undo stack, and does not bump
// Seq.
func (b *Buffer) Insert(pos Position, text string) (Edit, error) {
	b.mu.Lock()
	offset, err := b.byteOffsetLocked(pos)
	if err != nil {
		b.mu.Unlock()
		return Edit{}, err
	}
	if text == "" {
		b.mu.Unlock()
		return Edit{Range: Range{Start: pos, End: pos}}, nil
	}

	inserted := []byte(text)
	newData := make([]byte, 0, len(b.data)+len(inserted))
	newData = append(newData, b.data[:offset]...)
	newData = append(newData, inserted...)
	newData = append(newData, b.data[offset:]...)
	b.data = newData
	b.rebuildLineStarts()

	edit := Edit{
		Range:    Range{Start: pos, End: pos},
		Inserted: text,
	}
	b.undoStack = append(b.undoStack, edit)
	b.redoStack = nil
	b.seq++
	ch := Change{Kind: ChangeKindInsert, Edit: edit, Seq: b.seq}
	observers := append([]Observer(nil), b.observers...)
	b.mu.Unlock()

	notify(observers, ch)
	return edit, nil
}

// Delete removes the text in r. It returns the resulting Edit
// (with Range == r.Normalized() and Removed == the removed text)
// on success. Deleting an empty range is a successful no-op.
func (b *Buffer) Delete(r Range) (Edit, error) {
	r = r.Normalized()
	if r.Empty() {
		return Edit{Range: r}, nil
	}
	b.mu.Lock()
	startOff, err := b.byteOffsetLocked(r.Start)
	if err != nil {
		b.mu.Unlock()
		return Edit{}, err
	}
	endOff, err := b.byteOffsetLocked(r.End)
	if err != nil {
		b.mu.Unlock()
		return Edit{}, err
	}

	removed := string(b.data[startOff:endOff])
	newData := make([]byte, 0, len(b.data)-(endOff-startOff))
	newData = append(newData, b.data[:startOff]...)
	newData = append(newData, b.data[endOff:]...)
	b.data = newData
	b.rebuildLineStarts()

	edit := Edit{
		Range:   r,
		Removed: removed,
	}
	b.undoStack = append(b.undoStack, edit)
	b.redoStack = nil
	b.seq++
	ch := Change{Kind: ChangeKindDelete, Edit: edit, Seq: b.seq}
	observers := append([]Observer(nil), b.observers...)
	b.mu.Unlock()

	notify(observers, ch)
	return edit, nil
}

// Undo reverses the most recent edit. It returns the original (now
// reversed) Edit and true on success; on an empty undo stack it
// returns the zero Edit and false.
//
// A successful Undo bumps Seq and moves the edit to the redo stack.
func (b *Buffer) Undo() (Edit, bool) {
	b.mu.Lock()
	n := len(b.undoStack)
	if n == 0 {
		b.mu.Unlock()
		return Edit{}, false
	}
	edit := b.undoStack[n-1]
	b.undoStack = b.undoStack[:n-1]

	if !b.applyReverseLocked(edit) {
		// Buffer state is inconsistent with the recorded edit. This
		// should never happen because all mutation flows through
		// Buffer methods; defensively push the edit back so callers
		// can keep retrying or inspect history.
		b.undoStack = append(b.undoStack, edit)
		b.mu.Unlock()
		return Edit{}, false
	}
	b.redoStack = append(b.redoStack, edit)
	b.seq++
	ch := Change{Kind: ChangeKindUndo, Edit: edit, Seq: b.seq}
	observers := append([]Observer(nil), b.observers...)
	b.mu.Unlock()

	notify(observers, ch)
	return edit, true
}

// Redo reapplies the most recently undone edit. It returns the
// reapplied Edit and true on success; on an empty redo stack it
// returns the zero Edit and false.
func (b *Buffer) Redo() (Edit, bool) {
	b.mu.Lock()
	n := len(b.redoStack)
	if n == 0 {
		b.mu.Unlock()
		return Edit{}, false
	}
	edit := b.redoStack[n-1]
	b.redoStack = b.redoStack[:n-1]

	if !b.applyForwardLocked(edit) {
		b.redoStack = append(b.redoStack, edit)
		b.mu.Unlock()
		return Edit{}, false
	}
	b.undoStack = append(b.undoStack, edit)
	b.seq++
	ch := Change{Kind: ChangeKindRedo, Edit: edit, Seq: b.seq}
	observers := append([]Observer(nil), b.observers...)
	b.mu.Unlock()

	notify(observers, ch)
	return edit, true
}

// applyForwardLocked splices edit.Inserted in at edit.Range.Start,
// replacing edit.Removed. Used by Redo. b.mu must be held.
func (b *Buffer) applyForwardLocked(edit Edit) bool {
	startOff, err := b.byteOffsetLocked(edit.Range.Start)
	if err != nil {
		return false
	}
	removedLen := len(edit.Removed)
	newData := make([]byte, 0, len(b.data)-removedLen+len(edit.Inserted))
	newData = append(newData, b.data[:startOff]...)
	newData = append(newData, edit.Inserted...)
	newData = append(newData, b.data[startOff+removedLen:]...)
	b.data = newData
	b.rebuildLineStarts()
	return true
}

// applyReverseLocked splices edit.Removed in at edit.Range.Start,
// replacing edit.Inserted. Used by Undo. b.mu must be held.
func (b *Buffer) applyReverseLocked(edit Edit) bool {
	startOff, err := b.byteOffsetLocked(edit.Range.Start)
	if err != nil {
		return false
	}
	insertedLen := len(edit.Inserted)
	newData := make([]byte, 0, len(b.data)-insertedLen+len(edit.Removed))
	newData = append(newData, b.data[:startOff]...)
	newData = append(newData, edit.Removed...)
	newData = append(newData, b.data[startOff+insertedLen:]...)
	b.data = newData
	b.rebuildLineStarts()
	return true
}

// lineEndLocked returns the byte offset just past the last byte of line
// (excluding the trailing '\n'). For the last line, it returns the
// full length of the buffer. b.mu must be held.
func (b *Buffer) lineEndLocked(line int) int {
	if line+1 < len(b.lineStarts) {
		return b.lineStarts[line+1] - 1
	}
	return len(b.data)
}

// rebuildLineStarts recomputes the line-start offset cache from
// scratch. Called after each mutation. O(N) in buffer length.
//
// The cache always begins with 0 (start of the first line); each
// '\n' contributes the offset of the next byte (start of the next
// line) — including a "phantom" empty line after a trailing '\n'
// because that is the convention text editors render.
func (b *Buffer) rebuildLineStarts() {
	b.lineStarts = b.lineStarts[:0]
	b.lineStarts = append(b.lineStarts, 0)
	for i, c := range b.data {
		if c == '\n' {
			b.lineStarts = append(b.lineStarts, i+1)
		}
	}
}
