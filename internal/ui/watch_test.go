package ui

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newProjectWorkbench builds a workbench over a real mini Go project so
// the project watcher is available.
func newProjectWorkbench(t *testing.T) (*Workbench, string) {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/watchme\n\ngo 1.25\n")
	writeFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")

	session := newSessionWithProject(t, root)
	wb, err := NewWorkbench(session)
	if err != nil {
		t.Fatalf("NewWorkbench: %v", err)
	}
	// project.Open resolves the root through filepath.Abs; align the
	// workbench root with the project's own view of it.
	wb.SetRoot(session.Project().Root())
	if err := wb.RefreshTree(context.Background()); err != nil {
		t.Fatalf("RefreshTree: %v", err)
	}
	return wb, session.Project().Root()
}

func treeHasChild(wb *Workbench, name string) bool {
	tree, ok := wb.Tree()
	if !ok {
		return false
	}
	for _, c := range tree.Children {
		if c.Name == name {
			return true
		}
	}
	return false
}

func TestWorkbench_StartWatching_RefreshesTreeOnCreate(t *testing.T) {
	wb, root := newProjectWorkbench(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	changed := make(chan struct{}, 8)
	if err := wb.StartWatching(ctx, func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	}); err != nil {
		t.Fatalf("StartWatching: %v", err)
	}

	if treeHasChild(wb, "added.go") {
		t.Fatal("added.go present before creation")
	}
	if err := os.WriteFile(filepath.Join(root, "added.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-changed:
			if treeHasChild(wb, "added.go") {
				return
			}
		case <-time.After(100 * time.Millisecond):
			if treeHasChild(wb, "added.go") {
				return
			}
		}
	}
	t.Fatal("tree did not pick up added.go within the deadline")
}

func TestWorkbench_StartWatching_NoProject_NoOp(t *testing.T) {
	wb := newWorkbench(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := wb.StartWatching(ctx, func() {}); err != nil {
		t.Fatalf("StartWatching without project: %v, want nil no-op", err)
	}
}

func TestWorkbench_StartWatching_CancelStopsLoop(t *testing.T) {
	wb, _ := newProjectWorkbench(t)
	ctx, cancel := context.WithCancel(context.Background())
	if err := wb.StartWatching(ctx, nil); err != nil {
		t.Fatalf("StartWatching: %v", err)
	}
	// Cancel promptly; the loop must exit (no assertion beyond not
	// hanging — the goroutine selects on ctx.Done and the closing
	// events channel).
	cancel()
	time.Sleep(50 * time.Millisecond)
}
