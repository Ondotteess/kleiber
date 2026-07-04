// Package syntax provides lightweight, dependency-free syntax
// highlighting for source text shown in Kleiber's editor.
//
// Highlighting is purely lexical: HighlightGo tokenizes Go source
// with the standard library's go/scanner and classifies each token
// into a small set of Kinds (keywords, identifiers, predeclared
// builtins, strings, numbers, comments, operators). The result is
// organized per line as []Span slices whose byte columns follow the
// editor convention: zero-based, half-open [Start, End), columns
// measured in bytes. Multi-line tokens (raw string literals, general
// comments) are split into one span per line they cover, so a
// renderer never has to reason across line boundaries.
//
// The package is pure computation: no I/O, no goroutines, no
// logging. Invalid or incomplete source never fails — the scanner
// runs with a no-op error handler and whatever tokens it still
// recognizes are highlighted; the rest renders as plain text.
// Caching of results is the caller's concern.
package syntax
