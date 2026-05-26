package editor

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// --- Construction ----------------------------------------------------

func TestNewView_NilBuffer_Errors(t *testing.T) {
	_, err := NewView(nil)
	if !errors.Is(err, ErrViewBufferNil) {
		t.Errorf("err = %v, want ErrViewBufferNil", err)
	}
}

func TestNewView_DefaultsToDocumentStart(t *testing.T) {
	v, err := NewView(NewBuffer("hello\nworld"))
	if err != nil {
		t.Fatalf("NewView: %v", err)
	}
	sel := v.Selection()
	if !sel.IsEmpty() || !sel.Cursor.Equal(Position{0, 0}) {
		t.Errorf("default selection = %v, want caret at 0:0", sel)
	}
}

// --- Movement --------------------------------------------------------

func TestView_MoveTo_ClampsToBuffer(t *testing.T) {
	v, _ := NewView(NewBuffer("hello"))
	if err := v.MoveTo(Position{Line: 99, Column: 99}); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	sel := v.Selection()
	want := Position{Line: 0, Column: 5} // last line, end of line
	if !sel.Cursor.Equal(want) {
		t.Errorf("Cursor = %v, want %v", sel.Cursor, want)
	}
	if !sel.IsEmpty() {
		t.Error("MoveTo did not collapse the selection")
	}
}

func TestView_MoveCursorTo_PreservesAnchor(t *testing.T) {
	v, _ := NewView(NewBuffer("hello world"))
	if err := v.MoveTo(Position{0, 0}); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	if err := v.MoveCursorTo(Position{0, 5}); err != nil {
		t.Fatalf("MoveCursorTo: %v", err)
	}
	sel := v.Selection()
	if !sel.Anchor.Equal(Position{0, 0}) {
		t.Errorf("Anchor = %v, want 0:0", sel.Anchor)
	}
	if !sel.Cursor.Equal(Position{0, 5}) {
		t.Errorf("Cursor = %v, want 0:5", sel.Cursor)
	}
}

func TestView_MoveBy_AdvancesAndClamps(t *testing.T) {
	v, _ := NewView(NewBuffer("ab\ncd\nef"))
	if err := v.MoveTo(Position{1, 1}); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	if err := v.MoveBy(1, -1); err != nil {
		t.Fatalf("MoveBy: %v", err)
	}
	if sel := v.Selection(); !sel.Cursor.Equal(Position{2, 0}) {
		t.Errorf("Cursor = %v, want 2:0", sel.Cursor)
	}
	if err := v.MoveBy(99, 0); err != nil {
		t.Fatalf("MoveBy: %v", err)
	}
	// Clamp is axis-independent: line=101 collapses to the last line
	// (2), column stays at 0. Use MoveToLineEnd / MoveToDocumentEnd
	// to land at end-of-line / end-of-buffer.
	if sel := v.Selection(); !sel.Cursor.Equal(Position{2, 0}) {
		t.Errorf("Cursor after past-end MoveBy = %v, want 2:0", sel.Cursor)
	}
}

func TestView_Collapse_PutsCursorAtEnd(t *testing.T) {
	v, _ := NewView(NewBuffer("hello"))
	if err := v.SetSelection(NewSelection(Position{0, 1}, Position{0, 4})); err != nil {
		t.Fatalf("SetSelection: %v", err)
	}
	if err := v.Collapse(); err != nil {
		t.Fatalf("Collapse: %v", err)
	}
	sel := v.Selection()
	if !sel.IsEmpty() {
		t.Error("Collapse did not produce an empty selection")
	}
	if !sel.Cursor.Equal(Position{0, 4}) {
		t.Errorf("Cursor = %v, want 0:4", sel.Cursor)
	}
}

func TestView_MoveToLineStartEnd(t *testing.T) {
	v, _ := NewView(NewBuffer("hello\nworld"))
	if err := v.MoveTo(Position{1, 2}); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	if err := v.MoveToLineStart(); err != nil {
		t.Fatalf("MoveToLineStart: %v", err)
	}
	if got := v.Selection().Cursor; !got.Equal(Position{1, 0}) {
		t.Errorf("Cursor = %v, want 1:0", got)
	}
	if err := v.MoveToLineEnd(); err != nil {
		t.Fatalf("MoveToLineEnd: %v", err)
	}
	if got := v.Selection().Cursor; !got.Equal(Position{1, 5}) {
		t.Errorf("Cursor = %v, want 1:5", got)
	}
}

func TestView_MoveToDocumentStartEnd(t *testing.T) {
	v, _ := NewView(NewBuffer("ab\ncd"))
	if err := v.MoveTo(Position{0, 1}); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	if err := v.MoveToDocumentEnd(); err != nil {
		t.Fatalf("MoveToDocumentEnd: %v", err)
	}
	if got := v.Selection().Cursor; !got.Equal(Position{1, 2}) {
		t.Errorf("Cursor at doc end = %v, want 1:2", got)
	}
	if err := v.MoveToDocumentStart(); err != nil {
		t.Fatalf("MoveToDocumentStart: %v", err)
	}
	if got := v.Selection().Cursor; !got.Equal(Position{0, 0}) {
		t.Errorf("Cursor at doc start = %v, want 0:0", got)
	}
}

// --- InsertText ------------------------------------------------------

func TestView_InsertText_AtCaret_PlacesCursorAfter(t *testing.T) {
	b := NewBuffer("hello")
	v, _ := NewView(b)
	if err := v.MoveTo(Position{0, 5}); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	if _, err := v.InsertText(" world"); err != nil {
		t.Fatalf("InsertText: %v", err)
	}
	if b.Text() != "hello world" {
		t.Errorf("buffer = %q, want %q", b.Text(), "hello world")
	}
	if got := v.Selection().Cursor; !got.Equal(Position{0, 11}) {
		t.Errorf("Cursor = %v, want 0:11", got)
	}
}

func TestView_InsertText_ReplacesSelection(t *testing.T) {
	b := NewBuffer("hello world")
	v, _ := NewView(b)
	if err := v.SetSelection(NewSelection(Position{0, 6}, Position{0, 11})); err != nil {
		t.Fatalf("SetSelection: %v", err)
	}
	if _, err := v.InsertText("there"); err != nil {
		t.Fatalf("InsertText: %v", err)
	}
	if b.Text() != "hello there" {
		t.Errorf("buffer = %q, want %q", b.Text(), "hello there")
	}
	if got := v.Selection().Cursor; !got.Equal(Position{0, 11}) {
		t.Errorf("Cursor = %v, want 0:11", got)
	}
}

func TestView_InsertText_Multiline_AdvancesCursor(t *testing.T) {
	b := NewBuffer("abc")
	v, _ := NewView(b)
	if err := v.MoveTo(Position{0, 1}); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	if _, err := v.InsertText("X\nYZ"); err != nil {
		t.Fatalf("InsertText: %v", err)
	}
	if b.Text() != "aX\nYZbc" {
		t.Errorf("buffer = %q, want %q", b.Text(), "aX\nYZbc")
	}
	if got := v.Selection().Cursor; !got.Equal(Position{1, 2}) {
		t.Errorf("Cursor = %v, want 1:2", got)
	}
}

func TestView_InsertText_EmptyOnSelection_Deletes(t *testing.T) {
	b := NewBuffer("hello")
	v, _ := NewView(b)
	if err := v.SetSelection(NewSelection(Position{0, 1}, Position{0, 4})); err != nil {
		t.Fatalf("SetSelection: %v", err)
	}
	edit, err := v.InsertText("")
	if err != nil {
		t.Fatalf("InsertText: %v", err)
	}
	if edit.Removed != "ell" {
		t.Errorf("Removed = %q, want %q", edit.Removed, "ell")
	}
	if b.Text() != "ho" {
		t.Errorf("buffer = %q, want %q", b.Text(), "ho")
	}
	if got := v.Selection().Cursor; !got.Equal(Position{0, 1}) {
		t.Errorf("Cursor = %v, want 0:1", got)
	}
}

// --- Delete / Backspace ---------------------------------------------

func TestView_Delete_NoSelection_IsNoop(t *testing.T) {
	b := NewBuffer("hello")
	v, _ := NewView(b)
	if err := v.MoveTo(Position{0, 2}); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	edit, err := v.Delete()
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !edit.IsEmpty() {
		t.Errorf("Edit = %+v, want empty", edit)
	}
	if b.Text() != "hello" {
		t.Errorf("buffer = %q, want unchanged", b.Text())
	}
}

func TestView_Delete_RemovesSelection(t *testing.T) {
	b := NewBuffer("hello world")
	v, _ := NewView(b)
	if err := v.SetSelection(NewSelection(Position{0, 5}, Position{0, 11})); err != nil {
		t.Fatalf("SetSelection: %v", err)
	}
	if _, err := v.Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if b.Text() != "hello" {
		t.Errorf("buffer = %q, want %q", b.Text(), "hello")
	}
	if got := v.Selection().Cursor; !got.Equal(Position{0, 5}) {
		t.Errorf("Cursor = %v, want 0:5", got)
	}
}

func TestView_Backspace_DeletesByteBeforeCaret(t *testing.T) {
	b := NewBuffer("hello")
	v, _ := NewView(b)
	if err := v.MoveTo(Position{0, 3}); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	if _, err := v.Backspace(); err != nil {
		t.Fatalf("Backspace: %v", err)
	}
	if b.Text() != "helo" {
		t.Errorf("buffer = %q, want %q", b.Text(), "helo")
	}
	if got := v.Selection().Cursor; !got.Equal(Position{0, 2}) {
		t.Errorf("Cursor = %v, want 0:2", got)
	}
}

func TestView_Backspace_AcrossLine_JoinsLines(t *testing.T) {
	b := NewBuffer("ab\ncd")
	v, _ := NewView(b)
	if err := v.MoveTo(Position{1, 0}); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	if _, err := v.Backspace(); err != nil {
		t.Fatalf("Backspace: %v", err)
	}
	if b.Text() != "abcd" {
		t.Errorf("buffer = %q, want %q", b.Text(), "abcd")
	}
	if got := v.Selection().Cursor; !got.Equal(Position{0, 2}) {
		t.Errorf("Cursor = %v, want 0:2", got)
	}
}

func TestView_Backspace_AtDocumentStart_IsNoop(t *testing.T) {
	b := NewBuffer("hello")
	v, _ := NewView(b)
	edit, err := v.Backspace()
	if err != nil {
		t.Fatalf("Backspace: %v", err)
	}
	if !edit.IsEmpty() {
		t.Errorf("Edit = %+v, want empty", edit)
	}
	if b.Text() != "hello" {
		t.Errorf("buffer = %q, want unchanged", b.Text())
	}
}

// --- External-edit clamping ----------------------------------------

func TestView_ExternalInsert_ShiftsCursorOnSameLine(t *testing.T) {
	b := NewBuffer("hello")
	v, _ := NewView(b)
	if err := v.MoveTo(Position{0, 3}); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	// Insert before the cursor, directly on the buffer.
	if _, err := b.Insert(Position{0, 0}, "XX"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if got := v.Selection().Cursor; !got.Equal(Position{0, 5}) {
		t.Errorf("Cursor = %v, want 0:5 (shifted +2)", got)
	}
}

func TestView_ExternalInsert_BeforeCursor_AcrossLines(t *testing.T) {
	b := NewBuffer("hello")
	v, _ := NewView(b)
	if err := v.MoveTo(Position{0, 3}); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	// Insert "XX\nYY" at start: pushes the cursor down a line.
	if _, err := b.Insert(Position{0, 0}, "XX\nYY"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if got := v.Selection().Cursor; !got.Equal(Position{1, 5}) {
		t.Errorf("Cursor = %v, want 1:5", got)
	}
}

func TestView_ExternalInsert_AfterCursor_Unchanged(t *testing.T) {
	b := NewBuffer("hello")
	v, _ := NewView(b)
	if err := v.MoveTo(Position{0, 2}); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	if _, err := b.Insert(Position{0, 4}, "ZZ"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if got := v.Selection().Cursor; !got.Equal(Position{0, 2}) {
		t.Errorf("Cursor = %v, want unchanged 0:2", got)
	}
}

func TestView_ExternalDelete_BeforeCursor_ShiftsBack(t *testing.T) {
	b := NewBuffer("hello")
	v, _ := NewView(b)
	if err := v.MoveTo(Position{0, 4}); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	if _, err := b.Delete(Range{Start: Position{0, 0}, End: Position{0, 2}}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got := v.Selection().Cursor; !got.Equal(Position{0, 2}) {
		t.Errorf("Cursor = %v, want 0:2", got)
	}
}

func TestView_ExternalDelete_OverCursor_SnapsToStart(t *testing.T) {
	b := NewBuffer("hello")
	v, _ := NewView(b)
	if err := v.MoveTo(Position{0, 3}); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	if _, err := b.Delete(Range{Start: Position{0, 1}, End: Position{0, 4}}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got := v.Selection().Cursor; !got.Equal(Position{0, 1}) {
		t.Errorf("Cursor = %v, want 0:1 (snapped to deletion start)", got)
	}
}

func TestView_ExternalUndo_RestoresLogicalPosition(t *testing.T) {
	b := NewBuffer("hello")
	v, _ := NewView(b)
	if err := v.MoveTo(Position{0, 5}); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	if _, err := b.Insert(Position{0, 0}, "XX"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if got := v.Selection().Cursor; !got.Equal(Position{0, 7}) {
		t.Fatalf("Cursor after Insert = %v, want 0:7", got)
	}
	if _, ok := b.Undo(); !ok {
		t.Fatal("Undo: false")
	}
	if got := v.Selection().Cursor; !got.Equal(Position{0, 5}) {
		t.Errorf("Cursor after Undo = %v, want 0:5", got)
	}
}

// --- Observer plumbing ---------------------------------------------

func TestView_Observer_FiresOnMove(t *testing.T) {
	b := NewBuffer("hello")
	v, _ := NewView(b)

	var mu sync.Mutex
	var seen []SelectionChange
	v.Observe(func(ch SelectionChange) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, ch)
	})

	if err := v.MoveTo(Position{0, 3}); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 {
		t.Fatalf("observer fired %d times, want 1", len(seen))
	}
	if seen[0].Cause != SelectionCauseMove {
		t.Errorf("Cause = %v, want SelectionCauseMove", seen[0].Cause)
	}
	if !seen[0].New.Cursor.Equal(Position{0, 3}) {
		t.Errorf("New.Cursor = %v, want 0:3", seen[0].New.Cursor)
	}
}

func TestView_Observer_FiresOnSelfEdit(t *testing.T) {
	b := NewBuffer("hello")
	v, _ := NewView(b)

	var mu sync.Mutex
	var seen []SelectionChange
	v.Observe(func(ch SelectionChange) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, ch)
	})

	if err := v.MoveTo(Position{0, 5}); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	if _, err := v.InsertText("!"); err != nil {
		t.Fatalf("InsertText: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("observer fired %d times, want 2", len(seen))
	}
	if seen[1].Cause != SelectionCauseEdit {
		t.Errorf("Cause = %v, want SelectionCauseEdit", seen[1].Cause)
	}
}

func TestView_Observer_FiresOnExternalEdit(t *testing.T) {
	b := NewBuffer("hello")
	v, _ := NewView(b)
	if err := v.MoveTo(Position{0, 3}); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}

	got := make(chan SelectionChange, 1)
	v.Observe(func(ch SelectionChange) {
		got <- ch
	})

	if _, err := b.Insert(Position{0, 0}, "X"); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	select {
	case ch := <-got:
		if ch.Cause != SelectionCauseExternal {
			t.Errorf("Cause = %v, want SelectionCauseExternal", ch.Cause)
		}
		if !ch.New.Cursor.Equal(Position{0, 4}) {
			t.Errorf("New.Cursor = %v, want 0:4", ch.New.Cursor)
		}
	case <-time.After(time.Second):
		t.Fatal("observer did not fire within 1s")
	}
}

func TestView_NoEvent_IfSelectionUnchanged(t *testing.T) {
	b := NewBuffer("hello")
	v, _ := NewView(b)
	if err := v.MoveTo(Position{0, 2}); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}

	count := 0
	v.Observe(func(SelectionChange) { count++ })

	if err := v.MoveTo(Position{0, 2}); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	if count != 0 {
		t.Errorf("observer fired %d times, want 0 for no-op move", count)
	}
}

// --- Concurrency ---------------------------------------------------

func TestView_Concurrent_MoveAndEdit_NoRace(t *testing.T) {
	b := NewBuffer("0123456789")
	v, _ := NewView(b)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = v.MoveTo(Position{Line: 0, Column: i % 10})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_, _ = b.Insert(Position{0, 0}, "x")
			_, _ = b.Delete(Range{Start: Position{0, 0}, End: Position{0, 1}})
		}
	}()
	wg.Wait()

	// No assertion needed beyond "no panic / no -race trip".
	if v.Buffer() != b {
		t.Error("Buffer() does not return the bound buffer")
	}
}
