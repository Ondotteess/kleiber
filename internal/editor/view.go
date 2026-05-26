package editor

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

// ErrViewBufferNil is returned by NewView when the supplied *Buffer is nil.
var ErrViewBufferNil = errors.New("editor: view buffer is nil")

// SelectionChangeCause classifies what triggered a SelectionChange.
type SelectionChangeCause int

// SelectionChangeCause values. Do not renumber — callers compare and persist.
const (
	// SelectionCauseMove tags a change produced by cursor movement
	// (MoveTo, MoveCursorTo, MoveBy, Collapse, SetSelection).
	SelectionCauseMove SelectionChangeCause = iota + 1

	// SelectionCauseEdit tags a change produced by a buffer mutation
	// driven by this View (InsertText, Delete, Backspace).
	SelectionCauseEdit

	// SelectionCauseExternal tags a change produced by a buffer
	// mutation driven by something else (another View, the LSP layer,
	// a direct Buffer call) — the selection was transformed against
	// the edit to remain valid.
	SelectionCauseExternal
)

// String renders the cause as a lower-case label suitable for logs.
func (c SelectionChangeCause) String() string {
	switch c {
	case SelectionCauseMove:
		return "move"
	case SelectionCauseEdit:
		return "edit"
	case SelectionCauseExternal:
		return "external"
	default:
		return "unknown"
	}
}

// SelectionChange describes a single update to a View's Selection.
// It is the value delivered to SelectionObservers.
type SelectionChange struct {
	Old   Selection
	New   Selection
	Cause SelectionChangeCause
}

// SelectionObserver is invoked, in registration order, after every
// successful update to a View's Selection. Observers must not block —
// long-running consumers should forward the change to a buffered
// channel and process it in a separate goroutine. Like Buffer.Observe,
// observers are held for the lifetime of the View; there is no
// "Unobserve" in v1.
type SelectionObserver func(SelectionChange)

// View is a directed selection over a Buffer.
//
// A View owns a single primary Selection and reacts to buffer
// mutations: when text is inserted or deleted elsewhere in the
// buffer, the selection is transformed so it continues to point at
// the same logical content. The transform is symmetric across Insert,
// Delete, Undo, and Redo.
//
// Multi-cursor selections are intentionally out of scope for v1 — see
// the package doc-comment.
//
// A View is safe for concurrent use. Operations that mutate the
// buffer hold the View's lock across the call, and the internal
// observer detects self-driven mutations to avoid double-applying
// them. Different Views on the same Buffer transform their selections
// independently.
type View struct {
	buf *Buffer

	mu        sync.Mutex
	selection Selection

	// selfMutating is non-zero while a View method is driving a
	// Buffer mutation. The buffer observer checks it atomically and
	// skips the transform: the driving method sets the selection
	// explicitly when it returns.
	selfMutating atomic.Int32

	observersMu sync.Mutex
	observers   []SelectionObserver
}

// NewView constructs a View bound to buf with the caret at the start
// of the document. It registers the View's transform observer on buf
// at construction time; the registration lives for the View's
// lifetime. Returns ErrViewBufferNil if buf is nil.
func NewView(buf *Buffer) (*View, error) {
	if buf == nil {
		return nil, ErrViewBufferNil
	}
	v := &View{
		buf:       buf,
		selection: NewCaret(Position{Line: 0, Column: 0}),
	}
	buf.Observe(v.onBufferChange)
	return v, nil
}

// Buffer returns the *Buffer this View targets.
func (v *View) Buffer() *Buffer { return v.buf }

// Selection returns the View's current Selection.
func (v *View) Selection() Selection {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.selection
}

// Observe registers an observer fired on every selection change. A
// nil observer is silently ignored.
func (v *View) Observe(o SelectionObserver) {
	if o == nil {
		return
	}
	v.observersMu.Lock()
	v.observers = append(v.observers, o)
	v.observersMu.Unlock()
}

// SetSelection replaces the View's selection with sel after clamping
// both endpoints to the buffer's current contents. A position past
// the end of a line is clamped to the line's end; a line past the end
// of the buffer is clamped to the last line.
func (v *View) SetSelection(sel Selection) error {
	clamped := Selection{
		Anchor: clampPosition(v.buf, sel.Anchor),
		Cursor: clampPosition(v.buf, sel.Cursor),
	}
	return v.applyMove(clamped)
}

// MoveTo collapses the selection to a caret at p (clamped). The
// previous anchor is discarded.
func (v *View) MoveTo(p Position) error {
	return v.applyMove(NewCaret(clampPosition(v.buf, p)))
}

// MoveCursorTo moves the cursor end to p, preserving the anchor. Use
// this to extend or shrink an existing selection without resetting
// it. p is clamped to the buffer.
func (v *View) MoveCursorTo(p Position) error {
	v.mu.Lock()
	anchor := v.selection.Anchor
	v.mu.Unlock()
	return v.applyMove(Selection{
		Anchor: anchor,
		Cursor: clampPosition(v.buf, p),
	})
}

// MoveBy advances the cursor by deltaLine lines and deltaColumn bytes
// from its current position, collapsing the selection. The destination
// is clamped to the buffer; movement that would land out of bounds
// stops at the nearest valid position.
//
// MoveBy advances by raw byte columns within a line. Line-wrapped
// movement (visual rows, multi-byte characters, tab expansion) is the
// responsibility of higher layers — the editor engine operates on
// byte positions, not glyphs.
func (v *View) MoveBy(deltaLine, deltaColumn int) error {
	v.mu.Lock()
	cursor := v.selection.Cursor
	v.mu.Unlock()
	target := Position{
		Line:   cursor.Line + deltaLine,
		Column: cursor.Column + deltaColumn,
	}
	return v.MoveTo(target)
}

// Collapse collapses the selection to a caret at its cursor end.
func (v *View) Collapse() error {
	v.mu.Lock()
	sel := v.selection
	v.mu.Unlock()
	return v.applyMove(sel.Collapsed())
}

// InsertText replaces the current selection with text. The selection
// is collapsed to a caret immediately after the inserted run. An
// empty text input deletes the selection without inserting anything;
// an empty selection plus empty text is a successful no-op.
func (v *View) InsertText(text string) (Edit, error) {
	v.mu.Lock()
	sel := v.selection
	v.mu.Unlock()

	target := sel.AsRange()

	v.selfMutating.Add(1)
	defer v.selfMutating.Add(-1)

	var deleteEdit Edit
	if !target.Empty() {
		var err error
		deleteEdit, err = v.buf.Delete(target)
		if err != nil {
			return Edit{}, fmt.Errorf("view: deleting selection before insert: %w", err)
		}
	}

	if text == "" {
		// Selection collapsed to its start; no text to insert.
		caret := NewCaret(target.Start)
		if setErr := v.applyEdit(caret); setErr != nil {
			return deleteEdit, setErr
		}
		return deleteEdit, nil
	}

	insertEdit, err := v.buf.Insert(target.Start, text)
	if err != nil {
		return insertEdit, fmt.Errorf("view: inserting text: %w", err)
	}
	caret := NewCaret(endOfInsert(target.Start, text))
	if setErr := v.applyEdit(caret); setErr != nil {
		return insertEdit, setErr
	}
	return insertEdit, nil
}

// Delete removes the current selection. If the selection is already
// empty the call is a successful no-op (and Edit.IsEmpty() reports
// true). The selection is collapsed to a caret at the deleted range's
// start.
func (v *View) Delete() (Edit, error) {
	v.mu.Lock()
	sel := v.selection
	v.mu.Unlock()
	target := sel.AsRange()
	if target.Empty() {
		return Edit{Range: target}, nil
	}

	v.selfMutating.Add(1)
	defer v.selfMutating.Add(-1)

	edit, err := v.buf.Delete(target)
	if err != nil {
		return edit, fmt.Errorf("view: deleting selection: %w", err)
	}
	caret := NewCaret(target.Start)
	if setErr := v.applyEdit(caret); setErr != nil {
		return edit, setErr
	}
	return edit, nil
}

// Backspace deletes the current selection if it is non-empty; if the
// selection is an empty caret, it deletes the single byte immediately
// before the caret. At the start of the document Backspace is a
// successful no-op.
func (v *View) Backspace() (Edit, error) {
	v.mu.Lock()
	sel := v.selection
	v.mu.Unlock()
	if !sel.IsEmpty() {
		return v.Delete()
	}

	start, ok := positionBefore(v.buf, sel.Cursor)
	if !ok {
		return Edit{Range: Range{Start: sel.Cursor, End: sel.Cursor}}, nil
	}

	v.selfMutating.Add(1)
	defer v.selfMutating.Add(-1)

	edit, err := v.buf.Delete(Range{Start: start, End: sel.Cursor})
	if err != nil {
		return edit, fmt.Errorf("view: backspace: %w", err)
	}
	caret := NewCaret(start)
	if setErr := v.applyEdit(caret); setErr != nil {
		return edit, setErr
	}
	return edit, nil
}

// MoveToDocumentStart collapses the selection to a caret at (0, 0).
func (v *View) MoveToDocumentStart() error {
	return v.MoveTo(Position{Line: 0, Column: 0})
}

// MoveToDocumentEnd collapses the selection to a caret at the end of
// the buffer.
func (v *View) MoveToDocumentEnd() error {
	return v.MoveTo(endOfBuffer(v.buf))
}

// MoveToLineStart collapses the selection to a caret at column 0 of
// the cursor's current line.
func (v *View) MoveToLineStart() error {
	v.mu.Lock()
	line := v.selection.Cursor.Line
	v.mu.Unlock()
	return v.MoveTo(Position{Line: line, Column: 0})
}

// MoveToLineEnd collapses the selection to a caret at the end of the
// cursor's current line (clamped if the line is out of range).
func (v *View) MoveToLineEnd() error {
	v.mu.Lock()
	line := v.selection.Cursor.Line
	v.mu.Unlock()
	text, ok := v.buf.LineText(line)
	if !ok {
		return v.MoveToDocumentEnd()
	}
	return v.MoveTo(Position{Line: line, Column: len(text)})
}

// applyMove sets the selection to sel and emits SelectionCauseMove.
func (v *View) applyMove(sel Selection) error {
	return v.set(sel, SelectionCauseMove)
}

// applyEdit sets the selection to sel and emits SelectionCauseEdit.
// Used after a self-driven buffer mutation.
func (v *View) applyEdit(sel Selection) error {
	return v.set(sel, SelectionCauseEdit)
}

// set is the unconditional setter. It deduplicates no-op writes
// (sel == current) so observers do not see spurious events.
func (v *View) set(sel Selection, cause SelectionChangeCause) error {
	v.mu.Lock()
	old := v.selection
	if old.Equal(sel) {
		v.mu.Unlock()
		return nil
	}
	v.selection = sel
	v.mu.Unlock()
	v.notify(SelectionChange{Old: old, New: sel, Cause: cause})
	return nil
}

// onBufferChange is the View's Buffer.Observe callback.
//
// For self-driven mutations the View has already set the resulting
// selection — the observer detects this via selfMutating and returns
// without re-transforming.
//
// For external mutations the observer transforms the current
// selection against the change so it remains valid, then notifies
// subscribers with SelectionCauseExternal.
func (v *View) onBufferChange(ch Change) {
	if v.selfMutating.Load() > 0 {
		return
	}
	v.mu.Lock()
	old := v.selection
	transformed := transformSelection(old, ch)
	clamped := Selection{
		Anchor: clampPosition(v.buf, transformed.Anchor),
		Cursor: clampPosition(v.buf, transformed.Cursor),
	}
	if old.Equal(clamped) {
		v.mu.Unlock()
		return
	}
	v.selection = clamped
	v.mu.Unlock()
	v.notify(SelectionChange{Old: old, New: clamped, Cause: SelectionCauseExternal})
}

// notify fans out a SelectionChange to observers without holding the
// selection lock. Observers see a stable view of the change but may
// observe the View's selection move past it concurrently — design
// accordingly.
func (v *View) notify(ch SelectionChange) {
	v.observersMu.Lock()
	observers := make([]SelectionObserver, len(v.observers))
	copy(observers, v.observers)
	v.observersMu.Unlock()
	for _, o := range observers {
		o(ch)
	}
}

// --- Transform helpers ---------------------------------------------------

// transformSelection maps sel through the buffer change ch so that it
// continues to point at the same logical content. Both endpoints are
// transformed independently; the relative direction (forward vs
// reversed) is preserved.
func transformSelection(sel Selection, ch Change) Selection {
	return Selection{
		Anchor: transformPosition(sel.Anchor, ch),
		Cursor: transformPosition(sel.Cursor, ch),
	}
}

// textsForChange returns (pre, post) for ch, where the change can be
// described as "replace pre with post starting at ch.Edit.Range.Start".
//
// Insert: pre = "",            post = inserted text.
// Delete: pre = removed text,  post = "".
// Undo:   pre = inserted text, post = removed text (reverse of the original).
// Redo:   pre = removed text,  post = inserted text (reapply the original).
func textsForChange(ch Change) (pre, post string) {
	switch ch.Kind {
	case ChangeKindInsert:
		return "", ch.Edit.Inserted
	case ChangeKindDelete:
		return ch.Edit.Removed, ""
	case ChangeKindUndo:
		return ch.Edit.Inserted, ch.Edit.Removed
	case ChangeKindRedo:
		return ch.Edit.Removed, ch.Edit.Inserted
	default:
		return "", ""
	}
}

// transformPosition maps q through ch. Positions strictly before the
// splice are unchanged; positions inside the spliced range collapse
// to the new end; positions after the splice shift by the change in
// length (line- and column-wise).
func transformPosition(q Position, ch Change) Position {
	pre, post := textsForChange(ch)
	spliceStart := ch.Edit.Range.Start
	spliceEnd := endOfInsert(spliceStart, pre)
	newEnd := endOfInsert(spliceStart, post)

	if q.LessThan(spliceStart) {
		return q
	}
	// Inside [spliceStart, spliceEnd): snap to newEnd. For a
	// zero-width splice (pure insert) spliceEnd == spliceStart and
	// no position satisfies this, so we fall through to the shift —
	// which moves q == spliceStart to newEnd, consistent with the
	// "typing pushes the cursor right" convention.
	if q.LessThan(spliceEnd) {
		return newEnd
	}
	return shiftPosition(q, spliceEnd, newEnd)
}

// shiftPosition translates q from a coordinate system anchored at
// spliceEnd to one anchored at newEnd. Lines shift by the difference
// in spliceEnd.Line and newEnd.Line. Column only shifts when q sits
// on spliceEnd's line — positions on later lines keep their column.
func shiftPosition(q, spliceEnd, newEnd Position) Position {
	out := Position{
		Line:   q.Line - spliceEnd.Line + newEnd.Line,
		Column: q.Column,
	}
	if q.Line == spliceEnd.Line {
		out.Column = q.Column - spliceEnd.Column + newEnd.Column
	}
	return out
}

// endOfInsert returns the position immediately after inserting text
// starting at p. For empty text it returns p. For a single-line
// insert it advances Column by len(text). For a multi-line insert it
// advances Line by the newline count and sets Column to the length
// of the final line (trailing run after the last '\n').
func endOfInsert(p Position, text string) Position {
	if text == "" {
		return p
	}
	lines := strings.Count(text, "\n")
	if lines == 0 {
		return Position{Line: p.Line, Column: p.Column + len(text)}
	}
	lastNL := strings.LastIndex(text, "\n")
	return Position{
		Line:   p.Line + lines,
		Column: len(text) - lastNL - 1,
	}
}

// clampPosition returns p constrained to the buffer's current
// contents: negative axes clamp to zero, a line past the last clamps
// to the last line, and a column past the line's end clamps to the
// line length.
func clampPosition(b *Buffer, p Position) Position {
	lineCount := b.Lines()
	line := p.Line
	if line < 0 {
		line = 0
	}
	if line >= lineCount {
		line = lineCount - 1
	}
	text, _ := b.LineText(line)
	col := p.Column
	if col < 0 {
		col = 0
	}
	if col > len(text) {
		col = len(text)
	}
	return Position{Line: line, Column: col}
}

// positionBefore returns the position one byte before p, or (zero,
// false) if p is at (0, 0). At column 0 of a non-first line, it
// returns the end of the previous line.
func positionBefore(b *Buffer, p Position) (Position, bool) {
	if p.Line == 0 && p.Column == 0 {
		return Position{}, false
	}
	if p.Column > 0 {
		return Position{Line: p.Line, Column: p.Column - 1}, true
	}
	prev := p.Line - 1
	text, ok := b.LineText(prev)
	if !ok {
		return Position{}, false
	}
	return Position{Line: prev, Column: len(text)}, true
}

// endOfBuffer returns the position immediately after the last byte
// of b. For an empty buffer the result is (0, 0).
func endOfBuffer(b *Buffer) Position {
	last := b.Lines() - 1
	if last < 0 {
		return Position{}
	}
	text, _ := b.LineText(last)
	return Position{Line: last, Column: len(text)}
}
