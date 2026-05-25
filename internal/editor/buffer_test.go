package editor

import (
	"errors"
	"sync"
	"testing"
)

// --- NewBuffer / Text / Length / Lines ------------------------------

func TestBuffer_NewBuffer_Empty(t *testing.T) {
	b := NewBuffer("")
	if b.Text() != "" {
		t.Errorf("Text() = %q, want empty", b.Text())
	}
	if b.Length() != 0 {
		t.Errorf("Length() = %d, want 0", b.Length())
	}
	if b.Lines() != 1 {
		t.Errorf("Lines() = %d, want 1 (empty buffer has one empty line)", b.Lines())
	}
}

func TestBuffer_NewBuffer_LinesCount(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{"", 1},
		{"foo", 1},
		{"foo\n", 2},
		{"foo\nbar", 2},
		{"foo\nbar\n", 3},
		{"\n", 2},
		{"\n\n\n", 4},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			b := NewBuffer(tc.input)
			if got := b.Lines(); got != tc.want {
				t.Errorf("Lines(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

// --- LineText -------------------------------------------------------

func TestBuffer_LineText_Cases(t *testing.T) {
	b := NewBuffer("hello\nworld\n!")
	cases := []struct {
		line     int
		wantText string
		wantOK   bool
	}{
		{0, "hello", true},
		{1, "world", true},
		{2, "!", true},
		{-1, "", false},
		{3, "", false},
		{99, "", false},
	}
	for _, tc := range cases {
		got, ok := b.LineText(tc.line)
		if ok != tc.wantOK {
			t.Errorf("LineText(%d) ok = %v, want %v", tc.line, ok, tc.wantOK)
		}
		if got != tc.wantText {
			t.Errorf("LineText(%d) = %q, want %q", tc.line, got, tc.wantText)
		}
	}
}

func TestBuffer_LineText_TrailingNewline(t *testing.T) {
	b := NewBuffer("foo\n")
	text, ok := b.LineText(1)
	if !ok || text != "" {
		t.Errorf("LineText(1) = (%q, %v), want (\"\", true)", text, ok)
	}
}

// --- Insert ---------------------------------------------------------

func TestBuffer_Insert_Cases(t *testing.T) {
	cases := []struct {
		name    string
		initial string
		pos     Position
		text    string
		want    string
	}{
		{"into empty", "", Position{0, 0}, "hi", "hi"},
		{"at start", "abc", Position{0, 0}, "X", "Xabc"},
		{"in middle", "abc", Position{0, 1}, "X", "aXbc"},
		{"at end of line", "abc", Position{0, 3}, "X", "abcX"},
		{"creates newline", "abc", Position{0, 1}, "X\nY", "aX\nYbc"},
		{"at next line start", "abc\ndef", Position{1, 0}, "X", "abc\nXdef"},
		{"at end of last line", "abc\ndef", Position{1, 3}, "X", "abc\ndefX"},
		{"at end of file with trailing nl", "abc\n", Position{1, 0}, "x", "abc\nx"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBuffer(tc.initial)
			edit, err := b.Insert(tc.pos, tc.text)
			if err != nil {
				t.Fatalf("Insert: %v", err)
			}
			if b.Text() != tc.want {
				t.Errorf("Text() = %q, want %q", b.Text(), tc.want)
			}
			if edit.Inserted != tc.text {
				t.Errorf("edit.Inserted = %q, want %q", edit.Inserted, tc.text)
			}
			if edit.Removed != "" {
				t.Errorf("edit.Removed = %q, want empty", edit.Removed)
			}
		})
	}
}

func TestBuffer_Insert_EmptyString_IsNoOp(t *testing.T) {
	b := NewBuffer("abc")
	seqBefore := b.Seq()
	edit, err := b.Insert(Position{0, 1}, "")
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if b.Text() != "abc" {
		t.Errorf("Text() = %q, want %q", b.Text(), "abc")
	}
	if b.Seq() != seqBefore {
		t.Errorf("Seq() = %d, want %d (empty insert must not bump Seq)", b.Seq(), seqBefore)
	}
	if !edit.IsEmpty() {
		t.Errorf("edit = %+v, want empty edit", edit)
	}
	// Undo stack untouched.
	if _, ok := b.Undo(); ok {
		t.Error("Undo returned true after no-op insert")
	}
}

func TestBuffer_Insert_LineOutOfBounds(t *testing.T) {
	b := NewBuffer("abc")
	_, err := b.Insert(Position{5, 0}, "x")
	if !errors.Is(err, ErrLineOutOfBounds) {
		t.Errorf("err = %v, want ErrLineOutOfBounds", err)
	}
	if b.Text() != "abc" {
		t.Errorf("buffer mutated after failed insert: %q", b.Text())
	}
}

func TestBuffer_Insert_ColumnOutOfBounds(t *testing.T) {
	b := NewBuffer("abc")
	_, err := b.Insert(Position{0, 99}, "x")
	if !errors.Is(err, ErrColumnOutOfBounds) {
		t.Errorf("err = %v, want ErrColumnOutOfBounds", err)
	}
}

// --- Delete ---------------------------------------------------------

func TestBuffer_Delete_Cases(t *testing.T) {
	cases := []struct {
		name        string
		initial     string
		r           Range
		want        string
		wantRemoved string
	}{
		{"single char", "abc", Range{Position{0, 1}, Position{0, 2}}, "ac", "b"},
		{"across newline", "abc\ndef", Range{Position{0, 2}, Position{1, 1}}, "abef", "c\nd"},
		{"entire line content", "abc\ndef\nghi", Range{Position{1, 0}, Position{1, 3}}, "abc\n\nghi", "def"},
		{"line including newline", "abc\ndef\nghi", Range{Position{1, 0}, Position{2, 0}}, "abc\nghi", "def\n"},
		{"entire buffer", "abc", Range{Position{0, 0}, Position{0, 3}}, "", "abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBuffer(tc.initial)
			edit, err := b.Delete(tc.r)
			if err != nil {
				t.Fatalf("Delete: %v", err)
			}
			if b.Text() != tc.want {
				t.Errorf("Text() = %q, want %q", b.Text(), tc.want)
			}
			if edit.Removed != tc.wantRemoved {
				t.Errorf("edit.Removed = %q, want %q", edit.Removed, tc.wantRemoved)
			}
		})
	}
}

func TestBuffer_Delete_EmptyRange_IsNoOp(t *testing.T) {
	b := NewBuffer("abc")
	seqBefore := b.Seq()
	_, err := b.Delete(Range{Position{0, 1}, Position{0, 1}})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if b.Text() != "abc" {
		t.Errorf("Text() = %q, want unchanged", b.Text())
	}
	if b.Seq() != seqBefore {
		t.Errorf("Seq bumped by empty-range delete")
	}
}

func TestBuffer_Delete_SwappedRange_Normalized(t *testing.T) {
	b := NewBuffer("abc")
	// End < Start; Normalized should swap them.
	_, err := b.Delete(Range{Start: Position{0, 2}, End: Position{0, 0}})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if b.Text() != "c" {
		t.Errorf("Text() = %q, want %q", b.Text(), "c")
	}
}

func TestBuffer_Delete_OutOfBounds(t *testing.T) {
	b := NewBuffer("abc")
	_, err := b.Delete(Range{Start: Position{0, 0}, End: Position{0, 99}})
	if !errors.Is(err, ErrColumnOutOfBounds) {
		t.Errorf("err = %v, want ErrColumnOutOfBounds", err)
	}
	if b.Text() != "abc" {
		t.Errorf("buffer mutated after failed delete: %q", b.Text())
	}
}

// --- Undo / Redo -----------------------------------------------------

func TestBuffer_Undo_AfterInsert(t *testing.T) {
	b := NewBuffer("abc")
	if _, err := b.Insert(Position{0, 3}, "def"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	edit, ok := b.Undo()
	if !ok {
		t.Fatal("Undo returned false after one insert")
	}
	if b.Text() != "abc" {
		t.Errorf("Text after Undo = %q, want %q", b.Text(), "abc")
	}
	if edit.Inserted != "def" {
		t.Errorf("Undo returned edit with Inserted = %q, want %q", edit.Inserted, "def")
	}
}

func TestBuffer_Undo_AfterDelete(t *testing.T) {
	b := NewBuffer("hello")
	if _, err := b.Delete(Range{Position{0, 1}, Position{0, 4}}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if b.Text() != "ho" {
		t.Fatalf("after delete, Text = %q, want %q", b.Text(), "ho")
	}
	if _, ok := b.Undo(); !ok {
		t.Fatal("Undo returned false")
	}
	if b.Text() != "hello" {
		t.Errorf("Text after Undo = %q, want %q", b.Text(), "hello")
	}
}

func TestBuffer_Undo_OnEmptyStack(t *testing.T) {
	b := NewBuffer("abc")
	edit, ok := b.Undo()
	if ok {
		t.Errorf("Undo on empty stack returned ok=true, edit=%+v", edit)
	}
	if b.Text() != "abc" {
		t.Errorf("Text after no-op Undo = %q, want %q", b.Text(), "abc")
	}
}

func TestBuffer_UndoRedo_Chain(t *testing.T) {
	b := NewBuffer("")
	steps := []struct {
		pos  Position
		text string
	}{
		{Position{0, 0}, "a"},
		{Position{0, 1}, "b"},
		{Position{0, 2}, "c"},
	}
	for _, s := range steps {
		if _, err := b.Insert(s.pos, s.text); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}
	if b.Text() != "abc" {
		t.Fatalf("after inserts Text = %q, want abc", b.Text())
	}

	// Roll back to nothing.
	for i := 0; i < len(steps); i++ {
		if _, ok := b.Undo(); !ok {
			t.Fatalf("Undo #%d returned false", i)
		}
	}
	if b.Text() != "" {
		t.Errorf("Text after Undo chain = %q, want empty", b.Text())
	}

	// Roll forward.
	for i := 0; i < len(steps); i++ {
		if _, ok := b.Redo(); !ok {
			t.Fatalf("Redo #%d returned false", i)
		}
	}
	if b.Text() != "abc" {
		t.Errorf("Text after Redo chain = %q, want abc", b.Text())
	}
}

func TestBuffer_Redo_OnEmptyStack(t *testing.T) {
	b := NewBuffer("abc")
	_, ok := b.Redo()
	if ok {
		t.Error("Redo on empty stack returned true")
	}
}

func TestBuffer_NewEditClearsRedoStack(t *testing.T) {
	b := NewBuffer("ab")
	if _, err := b.Insert(Position{0, 2}, "c"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if _, ok := b.Undo(); !ok {
		t.Fatal("Undo: ok=false")
	}
	// A fresh edit while redo has content invalidates redo.
	if _, err := b.Insert(Position{0, 2}, "X"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if _, ok := b.Redo(); ok {
		t.Error("Redo succeeded after a divergent edit; expected stack to be cleared")
	}
}

// --- ByteOffset / PositionAt ----------------------------------------

func TestBuffer_ByteOffset_RoundTrip(t *testing.T) {
	texts := []string{
		"",
		"hello",
		"foo\nbar",
		"αβγ", // multi-byte UTF-8
		"line1\nline2\nline3",
		"\n\n\n",
		"trailing\n",
	}
	for _, text := range texts {
		t.Run(text, func(t *testing.T) {
			b := NewBuffer(text)
			for offset := 0; offset <= len(text); offset++ {
				pos, err := b.PositionAt(offset)
				if err != nil {
					t.Fatalf("PositionAt(%d) on %q: %v", offset, text, err)
				}
				back, err := b.ByteOffset(pos)
				if err != nil {
					t.Fatalf("ByteOffset(%v): %v", pos, err)
				}
				if back != offset {
					t.Errorf("roundtrip mismatch for offset %d in %q: got %d via %v",
						offset, text, back, pos)
				}
			}
		})
	}
}

func TestBuffer_ByteOffset_Bounds(t *testing.T) {
	b := NewBuffer("abc\ndef")
	cases := []struct {
		pos     Position
		wantErr error
	}{
		{Position{-1, 0}, ErrLineOutOfBounds},
		{Position{5, 0}, ErrLineOutOfBounds},
		{Position{0, -1}, ErrColumnOutOfBounds},
		{Position{0, 99}, ErrColumnOutOfBounds},
	}
	for _, tc := range cases {
		_, err := b.ByteOffset(tc.pos)
		if !errors.Is(err, tc.wantErr) {
			t.Errorf("ByteOffset(%v) err = %v, want %v", tc.pos, err, tc.wantErr)
		}
	}
}

func TestBuffer_PositionAt_OutOfBounds(t *testing.T) {
	b := NewBuffer("abc")
	if _, err := b.PositionAt(-1); err == nil {
		t.Error("PositionAt(-1) returned nil error")
	}
	if _, err := b.PositionAt(99); err == nil {
		t.Error("PositionAt(99) returned nil error")
	}
}

// --- Seq -------------------------------------------------------------

func TestBuffer_Seq_Monotonic(t *testing.T) {
	b := NewBuffer("a")
	s0 := b.Seq()
	if _, err := b.Insert(Position{0, 1}, "b"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	s1 := b.Seq()
	if _, ok := b.Undo(); !ok {
		t.Fatal("Undo: false")
	}
	s2 := b.Seq()
	if _, ok := b.Redo(); !ok {
		t.Fatal("Redo: false")
	}
	s3 := b.Seq()

	if !(s0 < s1 && s1 < s2 && s2 < s3) {
		t.Errorf("Seq sequence not monotonic: %d %d %d %d", s0, s1, s2, s3)
	}
}

func TestBuffer_ConcurrentInsert_SameBuffer(t *testing.T) {
	b := NewBuffer("")

	const inserts = 128
	var wg sync.WaitGroup
	for i := 0; i < inserts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := b.Insert(Position{0, 0}, "x"); err != nil {
				t.Errorf("Insert: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := b.Length(); got != inserts {
		t.Errorf("Length() = %d, want %d", got, inserts)
	}
	if got := b.Seq(); got != inserts {
		t.Errorf("Seq() = %d, want %d", got, inserts)
	}
}

// --- Insert/Delete return value -------------------------------------

func TestBuffer_Insert_ReturnsEditWithEmptyRange(t *testing.T) {
	b := NewBuffer("abc")
	edit, err := b.Insert(Position{0, 1}, "X")
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if !edit.Range.Empty() {
		t.Errorf("Insert edit.Range = %v, want empty range at insert position", edit.Range)
	}
	if edit.Range.Start != (Position{0, 1}) {
		t.Errorf("edit.Range.Start = %v, want Position{0,1}", edit.Range.Start)
	}
}

func TestBuffer_Delete_ReturnsEditWithDeletedRange(t *testing.T) {
	b := NewBuffer("abcdef")
	r := Range{Start: Position{0, 1}, End: Position{0, 4}}
	edit, err := b.Delete(r)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if edit.Range != r {
		t.Errorf("edit.Range = %v, want %v", edit.Range, r)
	}
	if edit.Removed != "bcd" {
		t.Errorf("edit.Removed = %q, want %q", edit.Removed, "bcd")
	}
}
