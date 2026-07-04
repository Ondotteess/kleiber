package ui

import (
	"unicode"
	"unicode/utf8"
)

// WordBoundsAt returns the half-open byte range [start, end) of the word
// containing byteCol in line, for double-click word selection. A word is a
// maximal run of Unicode letters, decimal digits and underscores. byteCol is
// a byte offset into line (clamped to [0, len(line)]) and is expected to fall
// on a rune boundary, as produced by ByteColForVisual.
//
// When the rune starting at byteCol is not a word rune (or byteCol is at end
// of line), the rune immediately before byteCol is tried instead, so a click
// just past the last character of a word still selects it. When neither
// neighbor is a word rune, both bounds equal the clamped byteCol.
func WordBoundsAt(line string, byteCol int) (start, end int) {
	if byteCol < 0 {
		byteCol = 0
	}
	if byteCol > len(line) {
		byteCol = len(line)
	}

	// Anchor on a word rune: the one at byteCol, else the one before it.
	anchor := byteCol
	if r, size := utf8.DecodeRuneInString(line[anchor:]); size == 0 || !isWordRune(r) {
		r, size := utf8.DecodeLastRuneInString(line[:anchor])
		if size == 0 || !isWordRune(r) {
			return byteCol, byteCol
		}
		anchor -= size
	}

	start = anchor
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(line[:start])
		if size == 0 || !isWordRune(r) {
			break
		}
		start -= size
	}
	end = anchor
	for end < len(line) {
		r, size := utf8.DecodeRuneInString(line[end:])
		if size == 0 || !isWordRune(r) {
			break
		}
		end += size
	}
	return start, end
}

// isWordRune reports whether r belongs to a word for double-click selection:
// a Unicode letter, a decimal digit, or underscore.
func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
