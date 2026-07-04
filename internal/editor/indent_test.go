package editor

import "testing"

func TestAutoIndent_Cases(t *testing.T) {
	cases := []struct {
		name     string
		prevLine string
		want     string
	}{
		{"empty line", "", ""},
		{"no indent", "x := 1", ""},
		{"spaces", "    x := 1", "    "},
		{"tabs", "\t\tx := 1", "\t\t"},
		{"mixed tabs and spaces", "\t  x := 1", "\t  "},
		{"trailing open brace", "\tif x {", "\t\t"},
		{"trailing open paren", "  call(", "  \t"},
		{"open brace then trailing spaces", "\tif x {  ", "\t\t"},
		{"comment-only line", "\t// note", "\t"},
		{"whitespace-only line", "  \t", "  \t"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AutoIndent(tc.prevLine); got != tc.want {
				t.Errorf("AutoIndent(%q) = %q, want %q", tc.prevLine, got, tc.want)
			}
		})
	}
}
