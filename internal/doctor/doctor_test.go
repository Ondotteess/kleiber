package doctor

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeCheck is a Check stub returning a preset Finding.
type fakeCheck struct {
	name    string
	finding Finding
	hook    func()
}

func (f *fakeCheck) Name() string { return f.name }
func (f *fakeCheck) Run(_ context.Context, _ string) Finding {
	if f.hook != nil {
		f.hook()
	}
	return f.finding
}

func TestDoctor_Run_PreservesCheckOrder(t *testing.T) {
	a := &fakeCheck{name: "a", finding: Finding{Severity: SeverityOK, Title: "A"}}
	b := &fakeCheck{name: "b", finding: Finding{Severity: SeverityWarning, Title: "B"}}
	c := &fakeCheck{name: "c", finding: Finding{Severity: SeverityError, Title: "C"}}

	d := New(nil, a, b, c)
	findings, err := d.Run(context.Background(), ".")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(findings) != 3 {
		t.Fatalf("len(findings) = %d, want 3", len(findings))
	}
	for i, want := range []string{"A", "B", "C"} {
		if findings[i].Title != want {
			t.Errorf("findings[%d].Title = %q, want %q", i, findings[i].Title, want)
		}
	}
}

func TestDoctor_Run_FillsMissingCheckName(t *testing.T) {
	a := &fakeCheck{name: "named-check", finding: Finding{Title: "no name in finding"}}
	d := New(nil, a)
	findings, err := d.Run(context.Background(), ".")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if findings[0].CheckName != "named-check" {
		t.Errorf("CheckName = %q, want %q (Doctor should fill it)", findings[0].CheckName, "named-check")
	}
}

func TestDoctor_Run_PreservesExplicitCheckName(t *testing.T) {
	a := &fakeCheck{name: "loop-name", finding: Finding{CheckName: "explicit-name", Title: "x"}}
	d := New(nil, a)
	findings, _ := d.Run(context.Background(), ".")
	if findings[0].CheckName != "explicit-name" {
		t.Errorf("CheckName = %q, want it kept as-is", findings[0].CheckName)
	}
}

func TestDoctor_Run_ContextCancel_StopsEarly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	a := &fakeCheck{name: "a", finding: Finding{Title: "A"}, hook: cancel}
	b := &fakeCheck{name: "b", finding: Finding{Title: "B"}}

	d := New(nil, a, b)
	findings, err := d.Run(ctx, ".")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if len(findings) != 1 {
		t.Errorf("len(findings) = %d, want 1 (only first check ran)", len(findings))
	}
}

func TestDoctor_Run_ResolvesRelativeRoot(t *testing.T) {
	var seen string
	probe := &fakeCheck{name: "probe", finding: Finding{Title: "ok"}, hook: nil}
	probe.hook = func() { /* placeholder; we override Run below */ }

	// Override Run via a wrapper that captures root.
	check := &captureRootCheck{name: "probe", captured: &seen}

	d := New(nil, check)
	if _, err := d.Run(context.Background(), "."); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if seen == "" {
		t.Fatal("Run did not invoke check")
	}
	if seen == "." {
		t.Errorf("Run passed relative root %q to check; expected absolute", seen)
	}
	if !strings.ContainsAny(seen, "/\\") {
		t.Errorf("Run passed %q to check; expected an absolute path", seen)
	}
}

type captureRootCheck struct {
	name     string
	captured *string
}

func (c *captureRootCheck) Name() string { return c.name }
func (c *captureRootCheck) Run(_ context.Context, root string) Finding {
	*c.captured = root
	return Finding{Severity: SeverityOK, Title: "ok"}
}

func TestDefaultChecks_ContainsAllThree(t *testing.T) {
	defaults := DefaultChecks()
	if len(defaults) != 3 {
		t.Fatalf("DefaultChecks len = %d, want 3", len(defaults))
	}
	names := make([]string, 0, len(defaults))
	for _, c := range defaults {
		names = append(names, c.Name())
	}
	want := []string{toolchainCheckName, toolsCheckName, workspaceCheckName}
	for i, w := range want {
		if names[i] != w {
			t.Errorf("DefaultChecks[%d] name = %q, want %q", i, names[i], w)
		}
	}
}
