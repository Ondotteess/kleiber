package lsp

import (
	"errors"
	"strings"
	"testing"
)

func TestPositionFromBytePosition(t *testing.T) {
	text := "ascii\nαβγ\nemoji 😀 x\n"
	cases := []struct {
		name   string
		line   int
		column int
		want   Position
	}{
		{
			name:   "ascii",
			line:   0,
			column: len("asc"),
			want:   Position{Line: 0, Character: 3},
		},
		{
			name:   "bmp unicode",
			line:   1,
			column: len("αβ"),
			want:   Position{Line: 1, Character: 2},
		},
		{
			name:   "non bmp counts as surrogate pair",
			line:   2,
			column: len("emoji 😀"),
			want:   Position{Line: 2, Character: len("emoji ") + 2},
		},
		{
			name:   "empty trailing line",
			line:   3,
			column: 0,
			want:   Position{Line: 3, Character: 0},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PositionFromBytePosition(text, tc.line, tc.column)
			if err != nil {
				t.Fatalf("PositionFromBytePosition: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestPositionFromBytePosition_OutOfBounds(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		line   int
		column int
	}{
		{"negative line", "x", -1, 0},
		{"negative column", "x", 0, -1},
		{"line too high", "x", 1, 0},
		{"column too high", "x", 0, 2},
		{"column splits rune", "α", 0, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := PositionFromBytePosition(tc.text, tc.line, tc.column)
			if !errors.Is(err, ErrPositionOutOfBounds) {
				t.Errorf("err = %v, want ErrPositionOutOfBounds", err)
			}
		})
	}
}

func TestPositionFromBytePosition_InvalidUTF8(t *testing.T) {
	_, err := PositionFromBytePosition(string([]byte{0xff, 'x'}), 0, 1)
	if !errors.Is(err, ErrInvalidUTF8) {
		t.Errorf("err = %v, want ErrInvalidUTF8", err)
	}
}

func TestRangeFromByteRange(t *testing.T) {
	text := "hello\nαβγ\nemoji 😀 x"
	got, err := RangeFromByteRange(
		text,
		1, len("α"),
		2, len("emoji 😀"),
	)
	if err != nil {
		t.Fatalf("RangeFromByteRange: %v", err)
	}
	want := Range{
		Start: Position{Line: 1, Character: 1},
		End:   Position{Line: 2, Character: len("emoji ") + 2},
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestRangeFromByteRange_StartError(t *testing.T) {
	_, err := RangeFromByteRange("x", 0, 2, 0, 0)
	if !errors.Is(err, ErrPositionOutOfBounds) {
		t.Fatalf("err = %v, want ErrPositionOutOfBounds", err)
	}
	if !strings.Contains(err.Error(), "start:") {
		t.Errorf("err = %v, want start context", err)
	}
}

func TestRangeFromByteRange_EndError(t *testing.T) {
	_, err := RangeFromByteRange("x", 0, 0, 0, 2)
	if !errors.Is(err, ErrPositionOutOfBounds) {
		t.Fatalf("err = %v, want ErrPositionOutOfBounds", err)
	}
	if !strings.Contains(err.Error(), "end:") {
		t.Errorf("err = %v, want end context", err)
	}
}
