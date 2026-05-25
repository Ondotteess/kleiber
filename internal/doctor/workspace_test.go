package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mkModule creates a minimal go.mod at dir with the given module path.
func mkModule(t *testing.T, dir, modulePath string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	content := "module " + modulePath + "\n\ngo 1.25\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(content), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
}

func TestWorkspaceCheck_SingleModule(t *testing.T) {
	root := t.TempDir()
	mkModule(t, root, "example.com/single")

	c := NewWorkspaceCheck()
	f := c.Run(context.Background(), root)
	if f.Severity != SeverityOK {
		t.Errorf("Severity = %v, want SeverityOK", f.Severity)
	}
	if !strings.Contains(f.Title, "single Go module") {
		t.Errorf("Title = %q, want it to mention single module", f.Title)
	}
}

func TestWorkspaceCheck_NoGoMod(t *testing.T) {
	root := t.TempDir() // empty
	c := NewWorkspaceCheck()
	f := c.Run(context.Background(), root)
	if f.Severity != SeverityInfo {
		t.Errorf("Severity = %v, want SeverityInfo", f.Severity)
	}
}

func TestWorkspaceCheck_MultiModuleNoGoWork(t *testing.T) {
	root := t.TempDir()
	mkModule(t, filepath.Join(root, "a"), "example.com/a")
	mkModule(t, filepath.Join(root, "b"), "example.com/b")
	mkModule(t, filepath.Join(root, "c"), "example.com/c")

	c := NewWorkspaceCheck()
	f := c.Run(context.Background(), root)
	if f.Severity != SeverityWarning {
		t.Errorf("Severity = %v, want SeverityWarning; title=%q", f.Severity, f.Title)
	}
	if !strings.Contains(f.Title, "3 Go modules") {
		t.Errorf("Title = %q, want it to mention 3 modules", f.Title)
	}
	if len(f.Fixes) != 1 {
		t.Fatalf("Fixes len = %d, want 1", len(f.Fixes))
	}
	cmd := f.Fixes[0].Command
	for _, want := range []string{"go work init", "./a", "./b", "./c"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("Fix command %q missing %q", cmd, want)
		}
	}
}

func TestWorkspaceCheck_MultiModuleWithGoWork(t *testing.T) {
	root := t.TempDir()
	mkModule(t, filepath.Join(root, "a"), "example.com/a")
	mkModule(t, filepath.Join(root, "b"), "example.com/b")
	// Minimal-but-valid go.work for this test (we don't parse it,
	// only its presence matters).
	if err := os.WriteFile(filepath.Join(root, "go.work"),
		[]byte("go 1.25\n\nuse (\n\t./a\n\t./b\n)\n"), 0o644); err != nil {
		t.Fatalf("write go.work: %v", err)
	}

	c := NewWorkspaceCheck()
	f := c.Run(context.Background(), root)
	if f.Severity != SeverityOK {
		t.Errorf("Severity = %v, want SeverityOK", f.Severity)
	}
	if !strings.Contains(f.Title, "go.work present") {
		t.Errorf("Title = %q, want it to mention go.work present", f.Title)
	}
}

func TestWorkspaceCheck_SkipsHiddenAndVendor(t *testing.T) {
	root := t.TempDir()
	// Real module at root level so the result is "single module"
	// (not flagged), proving the others were skipped.
	mkModule(t, root, "example.com/visible")
	// These should NOT count.
	mkModule(t, filepath.Join(root, ".hidden"), "example.com/hidden")
	mkModule(t, filepath.Join(root, "vendor", "pkg"), "example.com/v")
	mkModule(t, filepath.Join(root, "node_modules", "pkg"), "example.com/n")

	c := NewWorkspaceCheck()
	f := c.Run(context.Background(), root)
	if f.Severity != SeverityOK {
		t.Errorf("Severity = %v, want SeverityOK (only one visible module)", f.Severity)
	}
	if !strings.Contains(f.Title, "single Go module") {
		t.Errorf("Title = %q, want single-module title", f.Title)
	}
}

func TestFindAllGoMods_NestedDeeply(t *testing.T) {
	root := t.TempDir()
	mkModule(t, filepath.Join(root, "services", "api"), "example.com/api")
	mkModule(t, filepath.Join(root, "libs", "common"), "example.com/common")

	got, err := findAllGoMods(context.Background(), root)
	if err != nil {
		t.Fatalf("findAllGoMods: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2; got %v", len(got), got)
	}
}
