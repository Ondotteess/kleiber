package syntax

import (
	"path/filepath"
	"strings"
)

// Kind classifies the bytes covered by a Span for rendering purposes.
// The zero value KindText means plain, unstyled text.
type Kind int

// Kind values. Do not renumber — renderers and themes may persist
// mappings keyed by the numeric value.
const (
	// KindText is the default: plain text with no special styling.
	// Bytes not covered by any Span are implicitly KindText.
	KindText Kind = iota

	// KindKeyword covers Go keywords (func, return, if, ...).
	KindKeyword

	// KindIdentifier covers ordinary identifiers that are neither
	// keywords nor predeclared names.
	KindIdentifier

	// KindBuiltin covers identifiers predeclared in Go's universe
	// scope: built-in types (int, string, error, ...), constants
	// (true, false, iota), nil, and built-in functions (make, len,
	// append, ...).
	KindBuiltin

	// KindString covers string literals of all flavors: interpreted,
	// raw, and rune literals.
	KindString

	// KindNumber covers integer, floating-point, and imaginary
	// literals.
	KindNumber

	// KindComment covers line and general (block) comments.
	KindComment

	// KindOperator covers operators and punctuation (+, :=, braces,
	// explicit semicolons, ...).
	KindOperator
)

// String renders the kind as a lower-case label suitable for logs.
func (k Kind) String() string {
	switch k {
	case KindText:
		return "text"
	case KindKeyword:
		return "keyword"
	case KindIdentifier:
		return "identifier"
	case KindBuiltin:
		return "builtin"
	case KindString:
		return "string"
	case KindNumber:
		return "number"
	case KindComment:
		return "comment"
	case KindOperator:
		return "operator"
	default:
		return "unknown"
	}
}

// Span marks a half-open byte range [Start, End) within a single line
// and the Kind to render it as. Columns follow the editor convention:
// zero-based byte offsets into the line's text, which excludes the
// line's trailing newline (a carriage return before the newline
// counts as part of the line).
type Span struct {
	// Start is the byte column of the span's first byte.
	Start int

	// End is the byte column just past the span's last byte.
	End int

	// Kind says how to render the covered bytes.
	Kind Kind
}

// Language identifies a highlighting language. The zero value
// LanguagePlain means no highlighting.
type Language int

// Language values. Do not renumber — configuration and session state
// may persist them.
const (
	// LanguagePlain is the default: no highlighter is applied and
	// every line renders as plain text.
	LanguagePlain Language = iota

	// LanguageGo selects the Go highlighter (HighlightGo).
	LanguageGo
)

// String renders the language as a lower-case label suitable for logs.
func (l Language) String() string {
	switch l {
	case LanguagePlain:
		return "plain"
	case LanguageGo:
		return "go"
	default:
		return "unknown"
	}
}

// LanguageForPath picks the highlighting language for a file path
// from its extension. The comparison is case-insensitive so ".GO"
// files on case-insensitive filesystems highlight too. Unknown
// extensions map to LanguagePlain.
func LanguageForPath(path string) Language {
	if strings.EqualFold(filepath.Ext(path), ".go") {
		return LanguageGo
	}
	return LanguagePlain
}
