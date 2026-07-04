package syntax

import (
	"go/scanner"
	"go/token"
	"sort"
	"strings"
)

// HighlightGo tokenizes src as Go source and returns per-line
// highlight spans: element i holds the spans for zero-based line i,
// sorted by Start and non-overlapping. Bytes not covered by any span
// are plain text. The returned slice always has exactly one element
// per logical line of src, using the same line-counting convention
// as the editor's Buffer: lines are split on '\n' and a trailing
// newline yields a final empty line ("a\n" is two lines).
//
// Multi-line tokens (raw string literals, general comments) are
// split into one span per line they cover; covered lines with no
// bytes (for example a blank line inside a raw string) get no span.
//
// Invalid or incomplete source never fails: the scanner runs with a
// no-op error handler and whatever tokens it recognizes are still
// highlighted. Unrecognizable bytes are left as plain text.
func HighlightGo(src string) [][]Span {
	lineStarts := computeLineStarts(src)
	result := make([][]Span, len(lineStarts))

	fset := token.NewFileSet()
	file := fset.AddFile("", fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(file, []byte(src), nil, scanner.ScanComments)

	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		// The scanner inserts virtual SEMICOLON tokens with literal
		// "\n" at line ends and at EOF. They have no source extent
		// of their own (their position can even sit on a following
		// comment), so they must not produce spans.
		if tok == token.SEMICOLON && lit == "\n" {
			continue
		}
		kind := kindFor(tok, lit)
		if kind == KindText {
			continue
		}
		start := file.Offset(pos)
		end := tokenEnd(src, start, tok, lit)
		if end > len(src) {
			end = len(src)
		}
		if end <= start {
			continue
		}
		emitSpans(result, lineStarts, src, start, end, kind)
	}
	return result
}

// kindFor classifies a scanned token. Tokens that should stay plain
// (ILLEGAL, anything unrecognized) map to KindText.
func kindFor(tok token.Token, lit string) Kind {
	switch {
	case tok.IsKeyword():
		return KindKeyword
	case tok == token.IDENT:
		if isPredeclared(lit) {
			return KindBuiltin
		}
		return KindIdentifier
	case tok == token.STRING, tok == token.CHAR:
		return KindString
	case tok == token.INT, tok == token.FLOAT, tok == token.IMAG:
		return KindNumber
	case tok == token.COMMENT:
		return KindComment
	case tok.IsOperator():
		return KindOperator
	default:
		return KindText
	}
}

// tokenEnd computes the exclusive end offset in src of the token that
// starts at start. The scanner's literal string is not always the
// exact source extent — go/scanner strips carriage returns from raw
// string literals and comments — so those tokens are re-measured
// against the raw source bytes. Unterminated comments and raw
// strings extend to the end of the source.
func tokenEnd(src string, start int, tok token.Token, lit string) int {
	switch tok {
	case token.COMMENT:
		if strings.HasPrefix(src[start:], "//") {
			if i := strings.IndexByte(src[start:], '\n'); i >= 0 {
				return start + i
			}
			return len(src)
		}
		if i := strings.Index(src[start+2:], "*/"); i >= 0 {
			return start + 2 + i + 2
		}
		return len(src)
	case token.STRING:
		if start < len(src) && src[start] == '`' {
			if i := strings.IndexByte(src[start+1:], '`'); i >= 0 {
				return start + 1 + i + 1
			}
			return len(src)
		}
	}
	if lit != "" {
		return start + len(lit)
	}
	return start + len(tok.String())
}

// emitSpans appends spans for the token bytes [start, end) of src to
// every line the token covers, clipping each fragment to its line and
// translating absolute offsets to line-local byte columns. Fragments
// that clip to zero bytes (a covered line with no content) are
// dropped.
func emitSpans(result [][]Span, lineStarts []int, src string, start, end int, kind Kind) {
	line := lineAt(lineStarts, start)
	for line < len(lineStarts) && start < end {
		lineStart := lineStarts[line]
		lineEnd := len(src)
		if line+1 < len(lineStarts) {
			lineEnd = lineStarts[line+1] - 1 // exclude the trailing '\n'
		}
		segEnd := end
		if segEnd > lineEnd {
			segEnd = lineEnd
		}
		if segEnd > start {
			result[line] = append(result[line], Span{
				Start: start - lineStart,
				End:   segEnd - lineStart,
				Kind:  kind,
			})
		}
		if line+1 >= len(lineStarts) {
			break
		}
		start = lineStarts[line+1]
		line++
	}
}

// lineAt returns the index of the line containing byte offset off:
// the largest i with lineStarts[i] <= off.
func lineAt(lineStarts []int, off int) int {
	return sort.Search(len(lineStarts), func(i int) bool {
		return lineStarts[i] > off
	}) - 1
}

// computeLineStarts returns the byte offset of each line's first
// byte, mirroring the editor Buffer's semantics: the slice always
// begins with 0, each '\n' contributes the offset of the following
// byte, and a trailing '\n' therefore yields a final empty line.
func computeLineStarts(src string) []int {
	starts := make([]int, 1, strings.Count(src, "\n")+1)
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// isPredeclared reports whether name is declared in Go's universe
// scope: predeclared types, constants, nil, and built-in functions.
func isPredeclared(name string) bool {
	switch name {
	case "any", "bool", "byte", "comparable",
		"complex64", "complex128", "error",
		"float32", "float64",
		"int", "int8", "int16", "int32", "int64",
		"rune", "string",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"true", "false", "iota", "nil",
		"append", "cap", "clear", "close", "complex", "copy",
		"delete", "imag", "len", "make", "max", "min", "new",
		"panic", "print", "println", "real", "recover":
		return true
	}
	return false
}
