package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeGoMod creates a go.mod file at dir with the given go directive
// (or none, if goDir == ""). Returns dir for chaining.
func writeGoMod(t *testing.T, dir, modulePath, goDir string) string {
	t.Helper()
	content := "module " + modulePath + "\n"
	if goDir != "" {
		content += "\ngo " + goDir + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(content), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	return dir
}

func TestToolchainCheck_RuntimeMatchesGoMod(t *testing.T) {
	root := writeGoMod(t, t.TempDir(), "example.com/m", "1.25")
	c := NewToolchainCheck()
	c.runtimeVersion = func() string { return "go1.25.0" }

	f := c.Run(context.Background(), root)
	if f.Severity != SeverityOK {
		t.Errorf("Severity = %v, want SeverityOK; title=%q", f.Severity, f.Title)
	}
}

func TestToolchainCheck_RuntimeNewerThanGoMod(t *testing.T) {
	root := writeGoMod(t, t.TempDir(), "example.com/m", "1.21")
	c := NewToolchainCheck()
	c.runtimeVersion = func() string { return "go1.25.0" }

	f := c.Run(context.Background(), root)
	if f.Severity != SeverityOK {
		t.Errorf("Severity = %v, want SeverityOK (newer is fine)", f.Severity)
	}
}

func TestToolchainCheck_RuntimeOlderThanGoMod(t *testing.T) {
	root := writeGoMod(t, t.TempDir(), "example.com/m", "1.30")
	c := NewToolchainCheck()
	c.runtimeVersion = func() string { return "go1.25.0" }

	f := c.Run(context.Background(), root)
	if f.Severity != SeverityError {
		t.Errorf("Severity = %v, want SeverityError", f.Severity)
	}
	if !strings.Contains(f.Title, "too old") {
		t.Errorf("Title = %q, want it to mention 'too old'", f.Title)
	}
	if len(f.Fixes) == 0 {
		t.Error("expected a Fix suggesting Go download")
	}
}

func TestToolchainCheck_MissingGoDirective(t *testing.T) {
	root := writeGoMod(t, t.TempDir(), "example.com/m", "")
	c := NewToolchainCheck()
	c.runtimeVersion = func() string { return "go1.25.0" }

	f := c.Run(context.Background(), root)
	if f.Severity != SeverityInfo {
		t.Errorf("Severity = %v, want SeverityInfo", f.Severity)
	}
	if len(f.Fixes) != 1 {
		t.Fatalf("Fixes len = %d, want 1", len(f.Fixes))
	}
	if !strings.Contains(f.Fixes[0].Command, "go mod edit -go=1.25.0") {
		t.Errorf("Fix command = %q, want it to invoke `go mod edit -go=`", f.Fixes[0].Command)
	}
}

func TestToolchainCheck_NoGoMod(t *testing.T) {
	root := t.TempDir() // empty
	c := NewToolchainCheck()
	f := c.Run(context.Background(), root)
	if f.Severity != SeverityWarning {
		t.Errorf("Severity = %v, want SeverityWarning", f.Severity)
	}
	if !strings.Contains(f.Title, "go.mod") {
		t.Errorf("Title = %q, want it to mention go.mod", f.Title)
	}
}

func TestToolchainCheck_BrokenGoMod(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("this is not go.mod\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	c := NewToolchainCheck()
	f := c.Run(context.Background(), root)
	if f.Severity != SeverityError {
		t.Errorf("Severity = %v, want SeverityError; title=%q", f.Severity, f.Title)
	}
}

func TestToolchainCheck_FindsGoModInParent(t *testing.T) {
	parent := writeGoMod(t, t.TempDir(), "example.com/m", "1.25")
	child := filepath.Join(parent, "pkg")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	c := NewToolchainCheck()
	c.runtimeVersion = func() string { return "go1.25.0" }

	f := c.Run(context.Background(), child)
	if f.Severity != SeverityOK {
		t.Errorf("Severity = %v, want SeverityOK (found go.mod via parent walk)", f.Severity)
	}
}

func TestCompareGoVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.25", "1.25", 0},
		{"1.25.0", "1.25", 0},
		{"1.25", "1.25.1", -1},
		{"1.26", "1.25", 1},
		{"1.25rc1", "1.25", 0},
		{"1.25-beta", "1.25", 0},
		{"2.0", "1.99", 1},
	}
	for _, tc := range cases {
		got, err := compareGoVersion(tc.a, tc.b)
		if err != nil {
			t.Errorf("compareGoVersion(%q, %q): %v", tc.a, tc.b, err)
			continue
		}
		gotSign := sign(got)
		wantSign := sign(tc.want)
		if gotSign != wantSign {
			t.Errorf("compareGoVersion(%q, %q) = %d (sign %d), want sign %d",
				tc.a, tc.b, got, gotSign, wantSign)
		}
	}
}

func TestParseGoVersion_StripsPreRelease(t *testing.T) {
	cases := []struct {
		in   string
		want []int
	}{
		{"1.25", []int{1, 25}},
		{"1.25.0", []int{1, 25, 0}},
		{"1.25rc1", []int{1, 25}},
		{"1.25-beta", []int{1, 25}},
		{"1.21.5+something", []int{1, 21, 5}},
	}
	for _, tc := range cases {
		got, err := parseGoVersion(tc.in)
		if err != nil {
			t.Errorf("parseGoVersion(%q): %v", tc.in, err)
			continue
		}
		if !sliceEq(got, tc.want) {
			t.Errorf("parseGoVersion(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseGoVersion_RejectsEmpty(t *testing.T) {
	_, err := parseGoVersion("")
	if err == nil {
		t.Error("parseGoVersion(\"\"): nil error, want one")
	}
	_, err = parseGoVersion("rc1")
	if err == nil {
		t.Error("parseGoVersion(\"rc1\"): nil error, want one")
	}
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}

func sliceEq(a, b []int) bool {
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
