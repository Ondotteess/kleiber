package doctor

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestGoBinaryCheck_Present_ReportsVersion(t *testing.T) {
	c := NewGoBinaryCheck()
	c.lookPath = func(name string) (string, error) {
		if name == "go" {
			return "/fake/bin/go", nil
		}
		return "", exec.ErrNotFound
	}
	c.versionOf = func(_ context.Context, _ string) (string, error) {
		return "go1.26.3", nil
	}

	f := c.Run(context.Background(), "")
	if f.Severity != SeverityOK {
		t.Errorf("Severity = %v, want SeverityOK", f.Severity)
	}
	if !strings.Contains(f.Detail, "go1.26.3") {
		t.Errorf("Detail = %q, want it to mention go1.26.3", f.Detail)
	}
	if !strings.Contains(f.Detail, "/fake/bin/go") {
		t.Errorf("Detail = %q, want it to mention the resolved path", f.Detail)
	}
}

func TestGoBinaryCheck_Missing_IsErrorWithInstallHint(t *testing.T) {
	c := NewGoBinaryCheck()
	c.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }

	f := c.Run(context.Background(), "")
	if f.Severity != SeverityError {
		t.Errorf("Severity = %v, want SeverityError", f.Severity)
	}
	if len(f.Fixes) != 1 {
		t.Fatalf("Fixes len = %d, want 1", len(f.Fixes))
	}
	if !strings.Contains(f.Fixes[0].Command, "go.dev/dl") {
		t.Errorf("Fix command = %q, want a go.dev/dl install hint", f.Fixes[0].Command)
	}
}

func TestGoBinaryCheck_VersionProbeFailure_StillPresent(t *testing.T) {
	c := NewGoBinaryCheck()
	c.lookPath = func(string) (string, error) { return "/fake/bin/go", nil }
	c.versionOf = func(context.Context, string) (string, error) {
		return "", errors.New("probe failed")
	}

	f := c.Run(context.Background(), "")
	if f.Severity != SeverityOK {
		t.Errorf("Severity = %v, want SeverityOK (probe failure is non-fatal)", f.Severity)
	}
	if !strings.Contains(f.Detail, "at /fake/bin/go") {
		t.Errorf("Detail = %q, want path fallback when version is missing", f.Detail)
	}
}

func TestGoBinaryCheck_InDefaultChecks(t *testing.T) {
	var found bool
	for _, chk := range DefaultChecks() {
		if chk.Name() == goBinaryCheckName {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("DefaultChecks() does not include the go-binary check")
	}
}
