package ui

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Ondotteess/kleiber/internal/app"
)

func newWorkbench(t *testing.T) *Workbench {
	t.Helper()
	session := newRegisteredSession(t, app.Options{})
	wb, err := NewWorkbench(session)
	if err != nil {
		t.Fatalf("NewWorkbench: %v", err)
	}
	return wb
}

func TestNewWorkbench_NilSession(t *testing.T) {
	if _, err := NewWorkbench(nil); err == nil {
		t.Fatal("NewWorkbench(nil) error = nil, want ErrNilSession")
	}
}

func TestWorkbench_OpenFile_CreatesTabAndActivates(t *testing.T) {
	wb := newWorkbench(t)
	dir := t.TempDir()
	a := filepath.Join(dir, "a.go")
	b := filepath.Join(dir, "b.go")
	writeFile(t, a, "package a\n")
	writeFile(t, b, "package b\n")

	ctx := context.Background()
	if _, err := wb.OpenFile(ctx, a); err != nil {
		t.Fatalf("OpenFile a: %v", err)
	}
	if _, err := wb.OpenFile(ctx, b); err != nil {
		t.Fatalf("OpenFile b: %v", err)
	}

	if got := len(wb.Tabs()); got != 2 {
		t.Fatalf("len(Tabs) = %d, want 2", got)
	}
	if got := wb.ActiveIndex(); got != 1 {
		t.Errorf("ActiveIndex = %d, want 1 (last opened)", got)
	}
	active, ok := wb.ActiveTab()
	if !ok || active.Name != "b.go" {
		t.Errorf("ActiveTab = %+v ok=%v, want b.go", active, ok)
	}
}

func TestWorkbench_OpenFile_DedupesAlreadyOpen(t *testing.T) {
	wb := newWorkbench(t)
	dir := t.TempDir()
	a := filepath.Join(dir, "a.go")
	b := filepath.Join(dir, "b.go")
	writeFile(t, a, "package a\n")
	writeFile(t, b, "package b\n")

	ctx := context.Background()
	first, _ := wb.OpenFile(ctx, a)
	wb.OpenFile(ctx, b)

	// Re-opening a activates its existing tab rather than making a new one.
	again, err := wb.OpenFile(ctx, a)
	if err != nil {
		t.Fatalf("re-OpenFile a: %v", err)
	}
	if again.BufferID != first.BufferID {
		t.Errorf("re-open BufferID = %d, want same as first %d", again.BufferID, first.BufferID)
	}
	if got := len(wb.Tabs()); got != 2 {
		t.Errorf("len(Tabs) = %d, want 2 (no duplicate)", got)
	}
	if got := wb.ActiveIndex(); got != 0 {
		t.Errorf("ActiveIndex = %d, want 0 (re-activated first)", got)
	}
}

func TestWorkbench_CloseTab_AdjustsActive(t *testing.T) {
	wb := newWorkbench(t)
	dir := t.TempDir()
	ctx := context.Background()
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		p := filepath.Join(dir, name)
		writeFile(t, p, "package x\n")
		if _, err := wb.OpenFile(ctx, p); err != nil {
			t.Fatalf("OpenFile %s: %v", name, err)
		}
	}
	// active = 2 (c.go). Close it; active should clamp to 1 (b.go).
	if err := wb.CloseTab(2); err != nil {
		t.Fatalf("CloseTab: %v", err)
	}
	if got := len(wb.Tabs()); got != 2 {
		t.Fatalf("len(Tabs) = %d, want 2", got)
	}
	if got := wb.ActiveIndex(); got != 1 {
		t.Errorf("ActiveIndex = %d, want 1", got)
	}
}

func TestWorkbench_CloseTab_OutOfRange_NoOp(t *testing.T) {
	wb := newWorkbench(t)
	if err := wb.CloseTab(5); err != nil {
		t.Errorf("CloseTab out of range err = %v, want nil", err)
	}
}

func TestWorkbench_Activate_OutOfRange_Ignored(t *testing.T) {
	wb := newWorkbench(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "a.go")
	writeFile(t, p, "package a\n")
	wb.OpenFile(context.Background(), p)

	wb.Activate(99)
	if got := wb.ActiveIndex(); got != 0 {
		t.Errorf("ActiveIndex = %d, want 0 (out-of-range activate ignored)", got)
	}
}

func TestWorkbench_ActiveViewAndBuffer(t *testing.T) {
	wb := newWorkbench(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "a.go")
	writeFile(t, p, "package a\n")
	wb.OpenFile(context.Background(), p)

	view, err := wb.ActiveView()
	if err != nil || view == nil {
		t.Fatalf("ActiveView: %v", err)
	}
	buf, err := wb.ActiveBuffer()
	if err != nil || buf == nil {
		t.Fatalf("ActiveBuffer: %v", err)
	}
	if buf.Text() != "package a\n" {
		t.Errorf("buffer text = %q, want %q", buf.Text(), "package a\n")
	}
}

func TestWorkbench_NoActiveTab_Errors(t *testing.T) {
	wb := newWorkbench(t)
	if _, err := wb.ActiveView(); err == nil {
		t.Error("ActiveView with no tab: err = nil, want ErrNoActiveTab")
	}
	if _, ok := wb.ActiveTab(); ok {
		t.Error("ActiveTab with no tab: ok = true, want false")
	}
}

func TestWorkbench_Expand_Toggle(t *testing.T) {
	wb := newWorkbench(t)
	if !wb.IsExpanded(".") {
		t.Error("root should always be expanded")
	}
	if wb.IsExpanded("sub") {
		t.Error("sub should start collapsed")
	}
	wb.ToggleExpand("sub")
	if !wb.IsExpanded("sub") {
		t.Error("sub should be expanded after toggle")
	}
	wb.ToggleExpand("sub")
	if wb.IsExpanded("sub") {
		t.Error("sub should be collapsed after second toggle")
	}
	wb.SetExpanded("sub", true)
	if !wb.IsExpanded("sub") {
		t.Error("sub should be expanded after SetExpanded true")
	}
}

func TestWorkbench_RefreshTree_NoRoot_ClearsTree(t *testing.T) {
	wb := newWorkbench(t)
	if err := wb.RefreshTree(context.Background()); err != nil {
		t.Fatalf("RefreshTree: %v", err)
	}
	if _, ok := wb.Tree(); ok {
		t.Error("Tree loaded despite no root")
	}
}

func TestWorkbench_RefreshTree_BuildsFromRoot(t *testing.T) {
	wb := newWorkbench(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), "package main\n")
	writeFile(t, filepath.Join(dir, "README.md"), "# hi\n")

	wb.SetRoot(dir)
	if err := wb.RefreshTree(context.Background()); err != nil {
		t.Fatalf("RefreshTree: %v", err)
	}
	tree, ok := wb.Tree()
	if !ok {
		t.Fatal("Tree not loaded after RefreshTree with root")
	}
	// The tree includes non-Go files (README.md), which a project-analysis
	// tree would omit.
	var names []string
	for _, c := range tree.Children {
		names = append(names, c.Name)
	}
	if !containsString(names, "main.go") || !containsString(names, "README.md") {
		t.Errorf("tree children = %v, want main.go and README.md", names)
	}
}

func containsString(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestWorkbench_LSP_SetAndGet(t *testing.T) {
	wb := newWorkbench(t)
	if wb.LSP() != nil {
		t.Error("LSP should be nil before SetLSP")
	}
	c := NewLSPController(nil, wb.Engine())
	wb.SetLSP(c)
	if wb.LSP() != c {
		t.Error("LSP did not return the controller set by SetLSP")
	}
}

func TestWorkbench_Close_NilLSP_NoPanic(t *testing.T) {
	wb := newWorkbench(t)
	wb.Close() // no LSP attached; must be a safe no-op
}

func TestWorkbench_Close_ClosesController(t *testing.T) {
	wb := newWorkbench(t)
	wb.SetLSP(NewLSPController(nil, wb.Engine()))
	wb.Close() // closes the controller (nil supervisor) without hanging
}
