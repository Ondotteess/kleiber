package editor

import (
	"testing"
)

// --- FindAll ----------------------------------------------------------

func TestBuffer_FindAll_Cases(t *testing.T) {
	cases := []struct {
		name          string
		text          string
		query         string
		caseSensitive bool
		want          []Range
	}{
		{
			name:          "empty query yields nil",
			text:          "foo",
			query:         "",
			caseSensitive: true,
			want:          nil,
		},
		{
			name:          "no matches yields nil",
			text:          "foo bar",
			query:         "baz",
			caseSensitive: true,
			want:          nil,
		},
		{
			name:          "multiple matches on one line",
			text:          "foo bar foo",
			query:         "foo",
			caseSensitive: true,
			want: []Range{
				{Start: Position{0, 0}, End: Position{0, 3}},
				{Start: Position{0, 8}, End: Position{0, 11}},
			},
		},
		{
			name:          "matches across lines",
			text:          "foo\nbar foo\nfoo",
			query:         "foo",
			caseSensitive: true,
			want: []Range{
				{Start: Position{0, 0}, End: Position{0, 3}},
				{Start: Position{1, 4}, End: Position{1, 7}},
				{Start: Position{2, 0}, End: Position{2, 3}},
			},
		},
		{
			name:          "non-overlapping matches",
			text:          "aaaa",
			query:         "aa",
			caseSensitive: true,
			want: []Range{
				{Start: Position{0, 0}, End: Position{0, 2}},
				{Start: Position{0, 2}, End: Position{0, 4}},
			},
		},
		{
			name:          "query spanning a newline",
			text:          "foo\nbar",
			query:         "o\nb",
			caseSensitive: true,
			want: []Range{
				{Start: Position{0, 2}, End: Position{1, 1}},
			},
		},
		{
			name:          "case-sensitive excludes other case",
			text:          "Foo foo",
			query:         "foo",
			caseSensitive: true,
			want: []Range{
				{Start: Position{0, 4}, End: Position{0, 7}},
			},
		},
		{
			name:          "case-insensitive ascii",
			text:          "Foo foo FOO",
			query:         "foo",
			caseSensitive: false,
			want: []Range{
				{Start: Position{0, 0}, End: Position{0, 3}},
				{Start: Position{0, 4}, End: Position{0, 7}},
				{Start: Position{0, 8}, End: Position{0, 11}},
			},
		},
		{
			name:          "case-insensitive cyrillic",
			text:          "Привет мир\nпривет",
			query:         "ПРИВЕТ",
			caseSensitive: false,
			want: []Range{
				{Start: Position{0, 0}, End: Position{0, 12}},
				{Start: Position{1, 0}, End: Position{1, 12}},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBuffer(tc.text)
			got := b.FindAll(tc.query, tc.caseSensitive)
			if len(got) != len(tc.want) {
				t.Fatalf("FindAll(%q, %v) = %v, want %v",
					tc.query, tc.caseSensitive, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("match[%d] = %v, want %v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// --- NextMatch --------------------------------------------------------

func TestNextMatch_Cases(t *testing.T) {
	matches := []Range{
		{Start: Position{0, 0}, End: Position{0, 3}},
		{Start: Position{1, 4}, End: Position{1, 7}},
		{Start: Position{2, 0}, End: Position{2, 3}},
	}
	cases := []struct {
		name     string
		from     Position
		backward bool
		want     Range
	}{
		{"forward from before all", Position{0, 0}, false, matches[1]},
		{"forward from between", Position{1, 5}, false, matches[2]},
		{"forward wraps around", Position{2, 0}, false, matches[0]},
		{"backward from after all", Position{2, 5}, true, matches[2]},
		{"backward from between", Position{1, 4}, true, matches[0]},
		{"backward wraps around", Position{0, 0}, true, matches[2]},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := NextMatch(matches, tc.from, tc.backward)
			if !ok {
				t.Fatalf("NextMatch(%v, backward=%v) ok = false, want true",
					tc.from, tc.backward)
			}
			if got != tc.want {
				t.Errorf("NextMatch(%v, backward=%v) = %v, want %v",
					tc.from, tc.backward, got, tc.want)
			}
		})
	}
}

func TestNextMatch_EmptyInput_IsDeterministic(t *testing.T) {
	for _, backward := range []bool{false, true} {
		got, ok := NextMatch(nil, Position{0, 0}, backward)
		if ok {
			t.Errorf("NextMatch(nil, backward=%v) ok = true, want false", backward)
		}
		if got != (Range{}) {
			t.Errorf("NextMatch(nil, backward=%v) = %v, want zero Range", backward, got)
		}
	}
}
