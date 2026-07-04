package editor

import "strings"

// AutoIndent returns the indentation to insert on a new line opened
// after prevLine: prevLine's leading run of tabs and spaces, plus one
// extra tab when the whitespace-trimmed line ends with '{' or '(' —
// i.e. the previous line opened a block or argument list. It is a
// pure function of the line's text.
func AutoIndent(prevLine string) string {
	i := 0
	for i < len(prevLine) && (prevLine[i] == ' ' || prevLine[i] == '\t') {
		i++
	}
	indent := prevLine[:i]
	trimmed := strings.TrimSpace(prevLine)
	if strings.HasSuffix(trimmed, "{") || strings.HasSuffix(trimmed, "(") {
		return indent + "\t"
	}
	return indent
}
