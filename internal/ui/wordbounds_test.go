package ui

import "testing"

func TestWordBoundsAt_Expansion(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		byteCol   int
		wantStart int
		wantEnd   int
	}{
		{"empty line", "", 0, 0, 0},
		{"middle of ascii word", "foo bar baz", 5, 4, 7},
		{"start of line", "hello world", 0, 0, 5},
		{"first byte of word", "foo bar", 4, 4, 7},
		{"end of line after word", "foo bar", 7, 4, 7},
		{"just past word uses rune before", "foo(", 3, 0, 3},
		{"on punctuation after word", "foo.Bar", 3, 0, 3},
		{"after punctuation before word", "foo.Bar", 4, 4, 7},
		{"between two punctuation runes", "a..b", 2, 2, 2},
		{"on space with space before", "a  b", 2, 2, 2},
		{"underscore joins word", "my_var+1", 3, 0, 6},
		{"digits join word", "x42y!", 2, 0, 4},
		{"digits only", "  1234  ", 4, 2, 6},
		{"cyrillic middle", "функция(x)", 6, 0, 14}, // 7 two-byte runes
		{"cyrillic start", "привет мир", 0, 0, 12},
		{"cyrillic second word", "привет мир", 13, 13, 19},
		{"cyrillic end of line", "мир", 6, 0, 6},
		{"mixed ascii cyrillic", "abвг_1;", 4, 0, 8}, // a b + 2 two-byte runes + _ 1
		{"whole line is one word", "identifier", 5, 0, 10},
		{"negative clamps to zero", "abc", -3, 0, 3},
		{"past end clamps to len", "abc", 99, 0, 3},
		{"tab before word", "\tfoo", 1, 1, 4},
		{"on tab after word", "foo\tbar", 3, 0, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			start, end := WordBoundsAt(tc.line, tc.byteCol)
			if start != tc.wantStart || end != tc.wantEnd {
				t.Errorf("WordBoundsAt(%q, %d) = (%d, %d), want (%d, %d)",
					tc.line, tc.byteCol, start, end, tc.wantStart, tc.wantEnd)
			}
		})
	}
}

func TestWordBoundsAt_SelectsSubstring(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		byteCol int
		want    string
	}{
		{"ascii word", "if x_1 == y {", 4, "x_1"},
		{"cyrillic word", "вернуть значение", 15, "значение"},
		{"no word at punctuation", "a + b", 2, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			start, end := WordBoundsAt(tc.line, tc.byteCol)
			if got := tc.line[start:end]; got != tc.want {
				t.Errorf("WordBoundsAt(%q, %d) selects %q, want %q", tc.line, tc.byteCol, got, tc.want)
			}
		})
	}
}
