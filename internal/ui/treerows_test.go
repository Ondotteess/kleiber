package ui

import (
	"testing"

	"github.com/Ondotteess/kleiber/internal/project"
)

// sampleTree builds a small in-memory tree:
//
//	root/
//	  cmd/        (dir)
//	    main.go
//	  go.mod
func sampleTree() project.TreeNode {
	return project.TreeNode{
		Name: "root", Path: "/root", RelPath: ".", IsDir: true,
		Children: []project.TreeNode{
			{
				Name: "cmd", Path: "/root/cmd", RelPath: "cmd", IsDir: true,
				Children: []project.TreeNode{
					{Name: "main.go", Path: "/root/cmd/main.go", RelPath: "cmd/main.go"},
				},
			},
			{Name: "go.mod", Path: "/root/go.mod", RelPath: "go.mod"},
		},
	}
}

func injectTree(wb *Workbench, tree project.TreeNode) {
	wb.mu.Lock()
	wb.tree = tree
	wb.treeOK = true
	wb.mu.Unlock()
}

func TestWorkbench_VisibleRows_CollapsedByDefault(t *testing.T) {
	wb := newWorkbench(t)
	injectTree(wb, sampleTree())

	rows := wb.VisibleRows()
	// Root (always expanded) + its two direct children; cmd is collapsed
	// so main.go is hidden.
	want := []string{".", "cmd", "go.mod"}
	if got := relPaths(rows); !stringsEqual(got, want) {
		t.Errorf("VisibleRows relPaths = %v, want %v", got, want)
	}
	if rows[0].Depth != 0 || rows[1].Depth != 1 {
		t.Errorf("depths = %d,%d want 0,1", rows[0].Depth, rows[1].Depth)
	}
}

func TestWorkbench_VisibleRows_ExpandedDirShowsChildren(t *testing.T) {
	wb := newWorkbench(t)
	injectTree(wb, sampleTree())
	wb.SetExpanded("cmd", true)

	rows := wb.VisibleRows()
	want := []string{".", "cmd", "cmd/main.go", "go.mod"}
	if got := relPaths(rows); !stringsEqual(got, want) {
		t.Errorf("VisibleRows relPaths = %v, want %v", got, want)
	}
	// cmd/main.go is nested two deep.
	for _, r := range rows {
		if r.RelPath == "cmd/main.go" && r.Depth != 2 {
			t.Errorf("cmd/main.go depth = %d, want 2", r.Depth)
		}
	}
}

func TestWorkbench_VisibleRows_NoTree(t *testing.T) {
	wb := newWorkbench(t)
	if rows := wb.VisibleRows(); rows != nil {
		t.Errorf("VisibleRows with no tree = %v, want nil", rows)
	}
}

func relPaths(rows []TreeRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.RelPath
	}
	return out
}

func stringsEqual(a, b []string) bool {
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
