package ui

import "github.com/Ondotteess/kleiber/internal/syntax"

// Segment is a run of a single line that shares one syntax kind. A
// renderer maps Kind to a color; selection and diagnostic decorations are
// drawn separately as overlays computed from VisualColumn, so a Segment
// carries only text and kind.
type Segment struct {
	Text string
	Kind syntax.Kind
}

// AssembleLine splits line into color segments at the boundaries of the
// supplied syntax spans, filling the gaps between spans with plain
// KindText. spans must be sorted by Start and non-overlapping, as
// syntax.HighlightGo produces; out-of-range spans are clamped and empty
// ones skipped. An empty line yields no segments.
func AssembleLine(line string, spans []syntax.Span) []Segment {
	if line == "" {
		return nil
	}
	if len(spans) == 0 {
		return []Segment{{Text: line, Kind: syntax.KindText}}
	}

	segments := make([]Segment, 0, len(spans)*2+1)
	pos := 0
	for _, sp := range spans {
		start, end := sp.Start, sp.End
		if start < pos {
			start = pos
		}
		if end > len(line) {
			end = len(line)
		}
		if start >= end {
			continue
		}
		if start > pos {
			segments = append(segments, Segment{Text: line[pos:start], Kind: syntax.KindText})
		}
		segments = append(segments, Segment{Text: line[start:end], Kind: sp.Kind})
		pos = end
	}
	if pos < len(line) {
		segments = append(segments, Segment{Text: line[pos:], Kind: syntax.KindText})
	}
	return segments
}
