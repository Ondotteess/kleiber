package editor

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// FindAll returns every non-overlapping occurrence of query in the
// buffer, in document order. An empty query yields nil, as does a
// query with no matches. Matches may span line boundaries when query
// contains newlines. The returned Ranges describe the buffer contents
// at the moment of the call; concurrent mutations invalidate them.
//
// Case-insensitive matching folds rune-by-rune using Unicode simple
// case folding (the same relation strings.EqualFold implements)
// instead of lowering both strings up front: strings.ToLower can
// change a string's byte length (e.g. the Kelvin sign K folds to the
// one-byte 'k'), which would corrupt the byte-offset arithmetic
// behind the returned Ranges. Folding in place keeps every Range
// aligned with the buffer's actual bytes, including for multi-byte
// scripts such as Cyrillic.
func (b *Buffer) FindAll(query string, caseSensitive bool) []Range {
	if query == "" {
		return nil
	}
	text := b.Text()

	var spans [][2]int
	if caseSensitive {
		for from := 0; ; {
			i := strings.Index(text[from:], query)
			if i < 0 {
				break
			}
			start := from + i
			spans = append(spans, [2]int{start, start + len(query)})
			from = start + len(query)
		}
	} else {
		for i := 0; i < len(text); {
			if n, ok := foldPrefixLen(text[i:], query); ok {
				spans = append(spans, [2]int{i, i + n})
				i += n
				continue
			}
			_, size := utf8.DecodeRuneInString(text[i:])
			i += size
		}
	}
	if len(spans) == 0 {
		return nil
	}

	lineStarts := snapshotLineStarts(text)
	matches := make([]Range, len(spans))
	for i, s := range spans {
		matches[i] = Range{
			Start: snapshotPosition(lineStarts, s[0]),
			End:   snapshotPosition(lineStarts, s[1]),
		}
	}
	return matches
}

// NextMatch selects the match to jump to from position from. Moving
// forward it returns the first match whose start is strictly after
// from; moving backward, the last match whose start is strictly
// before from. When no match lies in the requested direction the
// search wraps around to the other end of matches. Empty input
// deterministically returns the zero Range and false.
//
// matches must be sorted in document order, as returned by FindAll.
func NextMatch(matches []Range, from Position, backward bool) (Range, bool) {
	if len(matches) == 0 {
		return Range{}, false
	}
	if backward {
		for i := len(matches) - 1; i >= 0; i-- {
			if matches[i].Start.LessThan(from) {
				return matches[i], true
			}
		}
		return matches[len(matches)-1], true
	}
	for _, m := range matches {
		if from.LessThan(m.Start) {
			return m, true
		}
	}
	return matches[0], true
}

// foldPrefixLen reports whether text begins with a case-folded match
// of query. On success it returns the length of the matched prefix in
// bytes of text — which can differ from len(query) when folding
// crosses byte-length boundaries.
func foldPrefixLen(text, query string) (int, bool) {
	ti := 0
	for qi := 0; qi < len(query); {
		if ti >= len(text) {
			return 0, false
		}
		qr, qs := utf8.DecodeRuneInString(query[qi:])
		tr, ts := utf8.DecodeRuneInString(text[ti:])
		if !foldEqual(qr, tr) {
			return 0, false
		}
		qi += qs
		ti += ts
	}
	return ti, true
}

// foldEqual reports whether two runes are equal under Unicode simple
// case folding, mirroring strings.EqualFold's per-rune comparison.
func foldEqual(a, b rune) bool {
	if a == b {
		return true
	}
	for r := unicode.SimpleFold(a); r != a; r = unicode.SimpleFold(r) {
		if r == b {
			return true
		}
	}
	return false
}

// snapshotLineStarts computes line-start byte offsets for a text
// snapshot, matching Buffer's line model (a trailing '\n' opens a
// final empty line).
func snapshotLineStarts(text string) []int {
	starts := make([]int, 1, 16)
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// snapshotPosition converts a byte offset within a snapshot into a
// Position using the snapshot's precomputed line starts.
func snapshotPosition(lineStarts []int, offset int) Position {
	low, high := 0, len(lineStarts)
	line := 0
	for low < high {
		mid := (low + high) / 2
		if lineStarts[mid] <= offset {
			line = mid
			low = mid + 1
		} else {
			high = mid
		}
	}
	return Position{Line: line, Column: offset - lineStarts[line]}
}
