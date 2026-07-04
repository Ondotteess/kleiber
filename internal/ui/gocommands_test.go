package ui

import (
	"path/filepath"
	"testing"

	"github.com/Ondotteess/kleiber/internal/project"
)

func TestGoCommands_BuiltIns(t *testing.T) {
	cmds := GoCommands()
	wantIDs := []string{"run", "build", "test", "tidy"}
	if len(cmds) != len(wantIDs) {
		t.Fatalf("GoCommands len = %d, want %d", len(cmds), len(wantIDs))
	}
	for i, cmd := range cmds {
		if cmd.ID != wantIDs[i] {
			t.Errorf("GoCommands[%d].ID = %q, want %q", i, cmd.ID, wantIDs[i])
		}
		if len(cmd.Args) == 0 || cmd.Args[0] != "go" {
			t.Errorf("GoCommands[%d].Args = %v, want it to start with go", i, cmd.Args)
		}
	}
}

func TestModuleDirForFile_LongestMatch(t *testing.T) {
	a := filepath.FromSlash("/work/a")
	ab := filepath.FromSlash("/work/a/b")
	modules := []project.Module{{Dir: a}, {Dir: ab}}

	tests := []struct {
		name string
		path string
		want string
	}{
		{"nested module wins", filepath.FromSlash("/work/a/b/x.go"), ab},
		{"outer module", filepath.FromSlash("/work/a/x.go"), a},
		{"module dir itself", ab, ab},
		{"outside any module", filepath.FromSlash("/other/x.go"), ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := moduleDirForFile(modules, tc.path); got != tc.want {
				t.Errorf("moduleDirForFile(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestCommandDir_NoProject_ReturnsRoot(t *testing.T) {
	wb := newWorkbench(t)
	root := t.TempDir()
	wb.SetRoot(root)
	if got := CommandDir(wb); got != root {
		t.Errorf("CommandDir = %q, want root %q", got, root)
	}
}
