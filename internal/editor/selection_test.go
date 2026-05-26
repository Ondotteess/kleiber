package editor

import "testing"

func TestSelection_NewCaret_IsEmpty(t *testing.T) {
	s := NewCaret(Position{1, 2})
	if !s.IsEmpty() {
		t.Error("caret reports not empty")
	}
	if !s.Anchor.Equal(s.Cursor) {
		t.Errorf("caret Anchor %v != Cursor %v", s.Anchor, s.Cursor)
	}
}

func TestSelection_IsReversed(t *testing.T) {
	cases := []struct {
		name string
		s    Selection
		want bool
	}{
		{"forward", NewSelection(Position{0, 0}, Position{0, 3}), false},
		{"reversed", NewSelection(Position{0, 3}, Position{0, 0}), true},
		{"caret", NewCaret(Position{2, 2}), false},
		{"forward across lines", NewSelection(Position{0, 1}, Position{2, 0}), false},
		{"reversed across lines", NewSelection(Position{2, 0}, Position{0, 1}), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s.IsReversed(); got != tc.want {
				t.Errorf("IsReversed() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSelection_AsRange(t *testing.T) {
	cases := []struct {
		name string
		s    Selection
		want Range
	}{
		{
			"forward",
			NewSelection(Position{0, 1}, Position{0, 4}),
			Range{Start: Position{0, 1}, End: Position{0, 4}},
		},
		{
			"reversed",
			NewSelection(Position{0, 4}, Position{0, 1}),
			Range{Start: Position{0, 1}, End: Position{0, 4}},
		},
		{
			"caret",
			NewCaret(Position{3, 3}),
			Range{Start: Position{3, 3}, End: Position{3, 3}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s.AsRange(); got != tc.want {
				t.Errorf("AsRange() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSelection_Collapsed(t *testing.T) {
	s := NewSelection(Position{0, 1}, Position{2, 5})
	c := s.Collapsed()
	if !c.IsEmpty() {
		t.Error("Collapsed() is not empty")
	}
	if !c.Cursor.Equal(s.Cursor) {
		t.Errorf("Collapsed().Cursor = %v, want %v", c.Cursor, s.Cursor)
	}
}

func TestSelection_CollapsedToStart(t *testing.T) {
	cases := []struct {
		name string
		s    Selection
		want Position
	}{
		{"forward", NewSelection(Position{0, 1}, Position{0, 4}), Position{0, 1}},
		{"reversed", NewSelection(Position{0, 4}, Position{0, 1}), Position{0, 1}},
		{"caret", NewCaret(Position{2, 2}), Position{2, 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := tc.s.CollapsedToStart()
			if !c.IsEmpty() {
				t.Error("CollapsedToStart() not empty")
			}
			if !c.Cursor.Equal(tc.want) {
				t.Errorf("CollapsedToStart().Cursor = %v, want %v", c.Cursor, tc.want)
			}
		})
	}
}

func TestSelection_MovedCursor_PreservesAnchor(t *testing.T) {
	s := NewSelection(Position{0, 0}, Position{0, 3})
	moved := s.MovedCursor(Position{1, 5})
	if !moved.Anchor.Equal(s.Anchor) {
		t.Errorf("Anchor changed: %v -> %v", s.Anchor, moved.Anchor)
	}
	if !moved.Cursor.Equal(Position{1, 5}) {
		t.Errorf("Cursor = %v, want {1,5}", moved.Cursor)
	}
}

func TestSelection_Equal(t *testing.T) {
	a := NewSelection(Position{0, 0}, Position{1, 1})
	if !a.Equal(a) {
		t.Error("Selection not Equal to itself")
	}
	b := NewSelection(Position{0, 0}, Position{1, 2})
	if a.Equal(b) {
		t.Error("distinct selections reported Equal")
	}
}

func TestSelection_String(t *testing.T) {
	s := NewSelection(Position{0, 0}, Position{1, 3})
	if got := s.String(); got != "1:1->2:4" {
		t.Errorf("String() = %q, want %q", got, "1:1->2:4")
	}
}
