package ui

import "testing"

func TestVisualColumn_TabsAndRunes(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		byteCol  int
		tabWidth int
		want     int
	}{
		{"empty", "", 0, 4, 0},
		{"ascii mid", "abcdef", 3, 4, 3},
		{"ascii end", "abc", 3, 4, 3},
		{"leading tab", "\tx", 1, 4, 4},
		{"tab then char", "\tx", 2, 4, 5},
		{"char then tab", "ab\t", 3, 4, 4},
		{"two tabs", "\t\t", 2, 4, 8},
		{"tab width 8", "\t", 1, 8, 8},
		{"partial tab stop", "abc\t", 4, 4, 4},
		{"seven then tab", "abcdefg\t", 8, 4, 8},
		{"multibyte rune", "фун", 4, 4, 2}, // 3 two-byte runes; byteCol 4 = after 2 runes
		{"non-positive tabwidth defaults", "\t", 1, 0, DefaultTabWidth},
		{"byteCol clamped", "ab", 99, 4, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := VisualColumn(tc.line, tc.byteCol, tc.tabWidth); got != tc.want {
				t.Errorf("VisualColumn(%q, %d, %d) = %d, want %d", tc.line, tc.byteCol, tc.tabWidth, got, tc.want)
			}
		})
	}
}

func TestByteColForVisual_MapsBack(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		visualCol int
		tabWidth  int
		want      int
	}{
		{"zero", "abc", 0, 4, 0},
		{"negative", "abc", -3, 4, 0},
		{"ascii exact", "abcdef", 3, 4, 3},
		{"past end", "abc", 10, 4, 3},
		{"before tab", "\tx", 0, 4, 0},
		{"inside tab snaps left", "\tx", 1, 4, 0},
		{"inside tab snaps right", "\tx", 3, 4, 1},
		{"after tab", "\tx", 4, 4, 1},
		{"multibyte boundary", "фун", 2, 4, 4}, // visual col 2 -> after 2nd rune (byte 4)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ByteColForVisual(tc.line, tc.visualCol, tc.tabWidth); got != tc.want {
				t.Errorf("ByteColForVisual(%q, %d, %d) = %d, want %d", tc.line, tc.visualCol, tc.tabWidth, got, tc.want)
			}
		})
	}
}

func TestVisualColumn_ByteColForVisual_RoundTrip(t *testing.T) {
	lines := []string{"", "package main", "\tfmt.Println(x)", "a\tb\tc", "функ"}
	for _, line := range lines {
		for byteCol := 0; byteCol <= len(line); byteCol++ {
			// Only test rune-boundary byte columns.
			if byteCol < len(line) && !runeBoundary(line, byteCol) {
				continue
			}
			vis := VisualColumn(line, byteCol, 4)
			back := ByteColForVisual(line, vis, 4)
			if VisualColumn(line, back, 4) != vis {
				t.Errorf("round trip failed: line=%q byteCol=%d vis=%d back=%d", line, byteCol, vis, back)
			}
		}
	}
}

func runeBoundary(s string, i int) bool {
	if i == 0 || i == len(s) {
		return true
	}
	return s[i]&0xC0 != 0x80
}

func TestVisualWidth(t *testing.T) {
	if got := VisualWidth("a\tb", 4); got != 5 {
		t.Errorf("VisualWidth = %d, want 5", got)
	}
}
