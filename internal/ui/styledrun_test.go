package ui

import (
	"testing"

	"github.com/Ondotteess/kleiber/internal/syntax"
)

func TestAssembleLine_Cases(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		spans []syntax.Span
		want  []Segment
	}{
		{
			name: "empty line",
			line: "",
			want: nil,
		},
		{
			name: "no spans is one plain segment",
			line: "hello",
			want: []Segment{{Text: "hello", Kind: syntax.KindText}},
		},
		{
			name:  "single keyword span",
			line:  "package",
			spans: []syntax.Span{{Start: 0, End: 7, Kind: syntax.KindKeyword}},
			want:  []Segment{{Text: "package", Kind: syntax.KindKeyword}},
		},
		{
			name:  "keyword then gap then ident",
			line:  "var x",
			spans: []syntax.Span{{Start: 0, End: 3, Kind: syntax.KindKeyword}, {Start: 4, End: 5, Kind: syntax.KindIdentifier}},
			want: []Segment{
				{Text: "var", Kind: syntax.KindKeyword},
				{Text: " ", Kind: syntax.KindText},
				{Text: "x", Kind: syntax.KindIdentifier},
			},
		},
		{
			name:  "trailing plain text after last span",
			line:  "x // c",
			spans: []syntax.Span{{Start: 2, End: 6, Kind: syntax.KindComment}},
			want: []Segment{
				{Text: "x ", Kind: syntax.KindText},
				{Text: "// c", Kind: syntax.KindComment},
			},
		},
		{
			name:  "span clamped to line length",
			line:  "ab",
			spans: []syntax.Span{{Start: 0, End: 99, Kind: syntax.KindString}},
			want:  []Segment{{Text: "ab", Kind: syntax.KindString}},
		},
		{
			name:  "empty span skipped",
			line:  "ab",
			spans: []syntax.Span{{Start: 1, End: 1, Kind: syntax.KindString}},
			want:  []Segment{{Text: "ab", Kind: syntax.KindText}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := AssembleLine(tc.line, tc.spans)
			if !segmentsEqual(got, tc.want) {
				t.Errorf("AssembleLine(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}

// TestAssembleLine_ReconstructsLine checks the invariant that concatenating
// segment texts reproduces the original line, over real highlighter output.
func TestAssembleLine_ReconstructsLine(t *testing.T) {
	src := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hi\") // go\n}\n"
	lineSpans := syntax.HighlightGo(src)
	lines := splitLinesForTest(src)
	if len(lineSpans) != len(lines) {
		t.Fatalf("line count mismatch: %d spans lines vs %d text lines", len(lineSpans), len(lines))
	}
	for i, line := range lines {
		segs := AssembleLine(line, lineSpans[i])
		var joined string
		for _, s := range segs {
			joined += s.Text
		}
		if joined != line {
			t.Errorf("line %d reconstruction = %q, want %q", i, joined, line)
		}
	}
}

func segmentsEqual(a, b []Segment) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func splitLinesForTest(src string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			lines = append(lines, src[start:i])
			start = i + 1
		}
	}
	lines = append(lines, src[start:])
	return lines
}
