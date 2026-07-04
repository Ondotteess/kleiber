package syntax

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Ondotteess/kleiber/internal/editor"
)

// --- Kind / Language ------------------------------------------------

func TestKind_String_Labels(t *testing.T) {
	cases := []struct {
		kind Kind
		want string
	}{
		{KindText, "text"},
		{KindKeyword, "keyword"},
		{KindIdentifier, "identifier"},
		{KindBuiltin, "builtin"},
		{KindString, "string"},
		{KindNumber, "number"},
		{KindComment, "comment"},
		{KindOperator, "operator"},
		{Kind(99), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.kind.String(); got != tc.want {
			t.Errorf("Kind(%d).String() = %q, want %q", int(tc.kind), got, tc.want)
		}
	}
}

func TestLanguage_String_Labels(t *testing.T) {
	cases := []struct {
		lang Language
		want string
	}{
		{LanguagePlain, "plain"},
		{LanguageGo, "go"},
		{Language(99), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.lang.String(); got != tc.want {
			t.Errorf("Language(%d).String() = %q, want %q", int(tc.lang), got, tc.want)
		}
	}
}

func TestLanguageForPath_Cases(t *testing.T) {
	cases := []struct {
		path string
		want Language
	}{
		{"main.go", LanguageGo},
		{"a/b/c.go", LanguageGo},
		{`C:\src\main.go`, LanguageGo},
		{"MAIN.GO", LanguageGo},
		{"main.txt", LanguagePlain},
		{"main.go.bak", LanguagePlain},
		{"go", LanguagePlain},
		{"", LanguagePlain},
	}
	for _, tc := range cases {
		if got := LanguageForPath(tc.path); got != tc.want {
			t.Errorf("LanguageForPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// --- HighlightGo: exact spans ----------------------------------------

func TestHighlightGo_Tokens(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want [][]Span
	}{
		{
			name: "keywords and identifiers",
			src:  "package main",
			want: [][]Span{{
				{Start: 0, End: 7, Kind: KindKeyword},
				{Start: 8, End: 12, Kind: KindIdentifier},
			}},
		},
		{
			name: "builtins",
			src:  "x := make([]int, 0)",
			want: [][]Span{{
				{Start: 0, End: 1, Kind: KindIdentifier},
				{Start: 2, End: 4, Kind: KindOperator},
				{Start: 5, End: 9, Kind: KindBuiltin},
				{Start: 9, End: 10, Kind: KindOperator},
				{Start: 10, End: 11, Kind: KindOperator},
				{Start: 11, End: 12, Kind: KindOperator},
				{Start: 12, End: 15, Kind: KindBuiltin},
				{Start: 15, End: 16, Kind: KindOperator},
				{Start: 17, End: 18, Kind: KindNumber},
				{Start: 18, End: 19, Kind: KindOperator},
			}},
		},
		{
			name: "numbers decimal hex float imaginary binary octal",
			src:  "a = 42 + 0x1F + 3.14 + 2i + 0b101 + 0o7",
			want: [][]Span{{
				{Start: 0, End: 1, Kind: KindIdentifier},
				{Start: 2, End: 3, Kind: KindOperator},
				{Start: 4, End: 6, Kind: KindNumber},
				{Start: 7, End: 8, Kind: KindOperator},
				{Start: 9, End: 13, Kind: KindNumber},
				{Start: 14, End: 15, Kind: KindOperator},
				{Start: 16, End: 20, Kind: KindNumber},
				{Start: 21, End: 22, Kind: KindOperator},
				{Start: 23, End: 25, Kind: KindNumber},
				{Start: 26, End: 27, Kind: KindOperator},
				{Start: 28, End: 33, Kind: KindNumber},
				{Start: 34, End: 35, Kind: KindOperator},
				{Start: 36, End: 39, Kind: KindNumber},
			}},
		},
		{
			name: "interpreted string and rune literal",
			src:  `s := "hi" + string('x')`,
			want: [][]Span{{
				{Start: 0, End: 1, Kind: KindIdentifier},
				{Start: 2, End: 4, Kind: KindOperator},
				{Start: 5, End: 9, Kind: KindString},
				{Start: 10, End: 11, Kind: KindOperator},
				{Start: 12, End: 18, Kind: KindBuiltin},
				{Start: 18, End: 19, Kind: KindOperator},
				{Start: 19, End: 22, Kind: KindString},
				{Start: 22, End: 23, Kind: KindOperator},
			}},
		},
		{
			name: "rune literal with escape",
			src:  `r := '\n'`,
			want: [][]Span{{
				{Start: 0, End: 1, Kind: KindIdentifier},
				{Start: 2, End: 4, Kind: KindOperator},
				{Start: 5, End: 9, Kind: KindString},
			}},
		},
		{
			name: "raw string across three lines",
			src:  "s := `a\nbb\nccc`\n",
			want: [][]Span{
				{
					{Start: 0, End: 1, Kind: KindIdentifier},
					{Start: 2, End: 4, Kind: KindOperator},
					{Start: 5, End: 7, Kind: KindString},
				},
				{{Start: 0, End: 2, Kind: KindString}},
				{{Start: 0, End: 4, Kind: KindString}},
				nil,
			},
		},
		{
			name: "line comment",
			src:  "x // hi\n",
			want: [][]Span{
				{
					{Start: 0, End: 1, Kind: KindIdentifier},
					{Start: 2, End: 7, Kind: KindComment},
				},
				nil,
			},
		},
		{
			name: "block comment across lines",
			src:  "a /* b\nc */ d",
			want: [][]Span{
				{
					{Start: 0, End: 1, Kind: KindIdentifier},
					{Start: 2, End: 6, Kind: KindComment},
				},
				{
					{Start: 0, End: 4, Kind: KindComment},
					{Start: 5, End: 6, Kind: KindIdentifier},
				},
			},
		},
		{
			name: "explicit semicolon is an operator",
			src:  "a; b",
			want: [][]Span{{
				{Start: 0, End: 1, Kind: KindIdentifier},
				{Start: 1, End: 2, Kind: KindOperator},
				{Start: 3, End: 4, Kind: KindIdentifier},
			}},
		},
		{
			name: "unicode identifier byte columns",
			src:  "var функция int",
			want: [][]Span{{
				{Start: 0, End: 3, Kind: KindKeyword},
				{Start: 4, End: 18, Kind: KindIdentifier},
				{Start: 19, End: 22, Kind: KindBuiltin},
			}},
		},
		{
			name: "crlf line endings",
			src:  "x := 1\r\ny := 2\r\n",
			want: [][]Span{
				{
					{Start: 0, End: 1, Kind: KindIdentifier},
					{Start: 2, End: 4, Kind: KindOperator},
					{Start: 5, End: 6, Kind: KindNumber},
				},
				{
					{Start: 0, End: 1, Kind: KindIdentifier},
					{Start: 2, End: 4, Kind: KindOperator},
					{Start: 5, End: 6, Kind: KindNumber},
				},
				nil,
			},
		},
		{
			name: "raw string with carriage return keeps byte columns",
			src:  "s := `a\r\nb`",
			want: [][]Span{
				{
					{Start: 0, End: 1, Kind: KindIdentifier},
					{Start: 2, End: 4, Kind: KindOperator},
					{Start: 5, End: 8, Kind: KindString},
				},
				{{Start: 0, End: 2, Kind: KindString}},
			},
		},
		{
			name: "empty source",
			src:  "",
			want: [][]Span{nil},
		},
		{
			name: "broken unterminated string",
			src:  `x := "abc`,
			want: [][]Span{{
				{Start: 0, End: 1, Kind: KindIdentifier},
				{Start: 2, End: 4, Kind: KindOperator},
				{Start: 5, End: 9, Kind: KindString},
			}},
		},
		{
			name: "broken illegal character stays plain",
			src:  "a @ b",
			want: [][]Span{{
				{Start: 0, End: 1, Kind: KindIdentifier},
				{Start: 4, End: 5, Kind: KindIdentifier},
			}},
		},
		{
			name: "broken unterminated block comment",
			src:  "/* never closed\nmore",
			want: [][]Span{
				{{Start: 0, End: 15, Kind: KindComment}},
				{{Start: 0, End: 4, Kind: KindComment}},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := HighlightGo(tc.src)
			if len(got) != len(tc.want) {
				t.Fatalf("HighlightGo(%q) returned %d lines, want %d", tc.src, len(got), len(tc.want))
			}
			for i := range got {
				if !slices.Equal(got[i], tc.want[i]) {
					t.Errorf("line %d spans = %v, want %v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// --- HighlightGo: line-count agreement with editor.Buffer ------------

func TestHighlightGo_LineCount_MatchesEditorBuffer(t *testing.T) {
	cases := []string{
		"",
		"a",
		"a\n",
		"a\nb",
		"a\nb\n",
		"\n",
		"\n\n",
		"x := 1\r\n",
		"package main\n\nfunc main() {}\n",
		"s := `raw\nstring\n`",
	}
	for _, src := range cases {
		t.Run(fmt.Sprintf("%q", src), func(t *testing.T) {
			got := len(HighlightGo(src))
			want := editor.NewBuffer(src).Lines()
			if got != want {
				t.Errorf("HighlightGo(%q) has %d lines, editor.Buffer has %d", src, got, want)
			}
		})
	}
}

// --- HighlightGo: span invariants over real source -------------------

func TestHighlightGo_PackageSource_SpanInvariants(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("Glob(*.go) failed: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no Go files found in package directory")
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile(%q) failed: %v", path, err)
			}
			src := string(data)
			got := HighlightGo(src)

			lines := strings.Split(src, "\n")
			if len(got) != len(lines) {
				t.Fatalf("HighlightGo returned %d lines, source has %d", len(got), len(lines))
			}
			if want := editor.NewBuffer(src).Lines(); len(got) != want {
				t.Fatalf("HighlightGo returned %d lines, editor.Buffer has %d", len(got), want)
			}
			for i, spans := range got {
				checkLineSpans(t, i, spans, len(lines[i]))
			}
		})
	}
}

// checkLineSpans asserts the per-line span invariants: spans are
// sorted by Start, non-overlapping, non-empty, inside the line, and
// carry a known non-text Kind.
func checkLineSpans(t *testing.T, line int, spans []Span, lineLen int) {
	t.Helper()
	prevEnd := 0
	for j, sp := range spans {
		if sp.Start < 0 {
			t.Errorf("line %d span %d: negative Start %d", line, j, sp.Start)
		}
		if sp.End <= sp.Start {
			t.Errorf("line %d span %d: empty or inverted span %v", line, j, sp)
		}
		if sp.Start < prevEnd {
			t.Errorf("line %d span %d: overlaps or unsorted (Start %d < previous End %d)",
				line, j, sp.Start, prevEnd)
		}
		if sp.End > lineLen {
			t.Errorf("line %d span %d: End %d past line length %d", line, j, sp.End, lineLen)
		}
		if sp.Kind == KindText || sp.Kind.String() == "unknown" {
			t.Errorf("line %d span %d: unexpected kind %v", line, j, sp.Kind)
		}
		prevEnd = sp.End
	}
}
