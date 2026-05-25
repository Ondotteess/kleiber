package doctor

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// stubLookPath returns a fake exec.LookPath that succeeds for tools in
// `found` (mapping name → resolved path) and returns exec.ErrNotFound
// otherwise.
func stubLookPath(found map[string]string) func(string) (string, error) {
	return func(name string) (string, error) {
		if p, ok := found[name]; ok {
			return p, nil
		}
		return "", exec.ErrNotFound
	}
}

// stubVersionOf returns a fake versionOf that maps tool path → version.
func stubVersionOf(versions map[string]string) func(ctx context.Context, path string, args []string) (string, error) {
	return func(_ context.Context, path string, _ []string) (string, error) {
		if v, ok := versions[path]; ok {
			return v, nil
		}
		return "", nil
	}
}

func TestToolsCheck_AllPresent(t *testing.T) {
	c := NewToolsCheck()
	c.lookPath = stubLookPath(map[string]string{
		"gopls": "/fake/bin/gopls",
		"dlv":   "/fake/bin/dlv",
	})
	c.versionOf = stubVersionOf(map[string]string{
		"/fake/bin/gopls": "v0.22.0",
		"/fake/bin/dlv":   "1.21.0",
	})

	f := c.Run(context.Background(), "")
	if f.Severity != SeverityOK {
		t.Errorf("Severity = %v, want SeverityOK", f.Severity)
	}
	if !strings.Contains(f.Detail, "gopls v0.22.0") {
		t.Errorf("Detail = %q, want it to mention gopls v0.22.0", f.Detail)
	}
	if !strings.Contains(f.Detail, "dlv 1.21.0") {
		t.Errorf("Detail = %q, want it to mention dlv 1.21.0", f.Detail)
	}
}

func TestToolsCheck_AllMissing(t *testing.T) {
	c := NewToolsCheck()
	c.lookPath = stubLookPath(nil)
	c.versionOf = stubVersionOf(nil)

	f := c.Run(context.Background(), "")
	if f.Severity != SeverityWarning {
		t.Errorf("Severity = %v, want SeverityWarning", f.Severity)
	}
	if !strings.Contains(f.Title, "gopls") || !strings.Contains(f.Title, "dlv") {
		t.Errorf("Title = %q, want it to list both gopls and dlv", f.Title)
	}
	if len(f.Fixes) != 2 {
		t.Fatalf("Fixes len = %d, want 2", len(f.Fixes))
	}
	for _, fix := range f.Fixes {
		if !strings.HasPrefix(fix.Command, "go install ") {
			t.Errorf("Fix command = %q, want `go install ...`", fix.Command)
		}
	}
}

func TestToolsCheck_PartialMissing(t *testing.T) {
	c := NewToolsCheck()
	c.lookPath = stubLookPath(map[string]string{
		"gopls": "/fake/bin/gopls",
	})
	c.versionOf = stubVersionOf(map[string]string{
		"/fake/bin/gopls": "v0.22.0",
	})

	f := c.Run(context.Background(), "")
	if f.Severity != SeverityWarning {
		t.Errorf("Severity = %v, want SeverityWarning", f.Severity)
	}
	if !strings.Contains(f.Title, "dlv") {
		t.Errorf("Title = %q, want it to mention dlv", f.Title)
	}
	if strings.Contains(f.Title, "gopls") {
		t.Errorf("Title = %q, should not call gopls missing", f.Title)
	}
	if len(f.Fixes) != 1 {
		t.Fatalf("Fixes len = %d, want 1 (only dlv missing)", len(f.Fixes))
	}
	if !strings.Contains(f.Fixes[0].Command, "dlv") {
		t.Errorf("Fix command = %q, want dlv install", f.Fixes[0].Command)
	}
}

func TestToolsCheck_VersionProbeFailure_StillReportsPresent(t *testing.T) {
	c := NewToolsCheck()
	c.lookPath = stubLookPath(map[string]string{
		"gopls": "/fake/bin/gopls",
		"dlv":   "/fake/bin/dlv",
	})
	c.versionOf = func(_ context.Context, _ string, _ []string) (string, error) {
		return "", errors.New("subprocess failed")
	}
	f := c.Run(context.Background(), "")
	if f.Severity != SeverityOK {
		t.Errorf("Severity = %v, want SeverityOK (version probe failure is non-fatal)", f.Severity)
	}
	if !strings.Contains(f.Detail, "at /fake/bin/gopls") {
		t.Errorf("Detail = %q, want path fallback when version is missing", f.Detail)
	}
}

func TestExtractVersion_GoplsFormat(t *testing.T) {
	out := "golang.org/x/tools/gopls v0.22.0\n    cached module: ...\n"
	if got := extractVersion(out); got != "v0.22.0" {
		t.Errorf("extractVersion = %q, want v0.22.0", got)
	}
}

func TestExtractVersion_DlvFormat(t *testing.T) {
	out := "Delve Debugger\nVersion: 1.21.0\nBuild: $Id: abc\n"
	if got := extractVersion(out); got != "1.21.0" {
		t.Errorf("extractVersion = %q, want 1.21.0", got)
	}
}

func TestExtractVersion_NoVersion(t *testing.T) {
	out := "not a version here\nhello world"
	if got := extractVersion(out); got != "" {
		t.Errorf("extractVersion = %q, want empty", got)
	}
}

func TestIsVersionToken(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{"v0.22.0", true},
		{"1.21.0", true},
		{"1.0", true},
		{"v1.21-beta1", true},
		{"v1.0+meta", true},
		{"v1+meta", false}, // requires both a digit and a dot before any suffix
		{"", false},
		{"v", false},
		{"123", false},
		{"abc", false},
		{"$Id:", false},
		{"golang.org", false},
	}
	for _, tc := range cases {
		if got := isVersionToken(tc.s); got != tc.want {
			t.Errorf("isVersionToken(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}
