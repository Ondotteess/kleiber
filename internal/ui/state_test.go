package ui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"github.com/Ondotteess/kleiber/internal/app"
	"github.com/Ondotteess/kleiber/internal/project"
)

func TestBuildState_NilSessionError(t *testing.T) {
	_, err := BuildState(nil)
	if !errors.Is(err, ErrNilSession) {
		t.Fatalf("BuildState err = %v, want ErrNilSession", err)
	}
}

func TestBuildState_NoProjectValidAndCommandPaletteSorted(t *testing.T) {
	session := newRegisteredSession(t, app.Options{})
	state, err := BuildState(session)
	if err != nil {
		t.Fatalf("BuildState: %v", err)
	}
	if state.Project.Open {
		t.Fatalf("Project.Open = true, want false without project")
	}
	if len(state.Commands) == 0 {
		t.Fatal("Commands is empty")
	}
	names := make([]string, len(state.Commands))
	for i, cmd := range state.Commands {
		names[i] = cmd.Name
		if cmd.Description == "" {
			t.Fatalf("command %s has empty description", cmd.Name)
		}
	}
	if !sort.StringsAreSorted(names) {
		t.Fatalf("commands not sorted: %v", names)
	}
}

func TestBuildState_BuffersAndViewsFromSession(t *testing.T) {
	session := newRegisteredSession(t, app.Options{})
	path := filepath.Join(t.TempDir(), "main.go")
	writeFile(t, path, "package main\n")

	if err := session.Dispatcher().Dispatch(context.Background(), app.CommandOpenFile, map[string]any{"path": path}); err != nil {
		t.Fatalf("Dispatch openFile: %v", err)
	}
	buffers := session.Buffers()
	if len(buffers) != 1 {
		t.Fatalf("session buffers = %d, want 1", len(buffers))
	}
	if err := session.Dispatcher().Dispatch(context.Background(), app.CommandNewView, map[string]any{
		"bufferID": json.Number(strconv.FormatInt(int64(buffers[0].ID), 10)),
	}); err != nil {
		t.Fatalf("Dispatch newView: %v", err)
	}

	state, err := BuildState(session)
	if err != nil {
		t.Fatalf("BuildState: %v", err)
	}
	if len(state.Buffers) != 1 {
		t.Fatalf("Buffers len = %d, want 1", len(state.Buffers))
	}
	if state.Buffers[0].Path == "" || state.Buffers[0].DisplayName != "main.go" {
		t.Fatalf("unexpected buffer item: %+v", state.Buffers[0])
	}
	if len(state.Views) != 1 {
		t.Fatalf("Views len = %d, want 1", len(state.Views))
	}
	if state.Views[0].BufferID != state.Buffers[0].ID {
		t.Fatalf("View.BufferID = %d, want %d", state.Views[0].BufferID, state.Buffers[0].ID)
	}
}

func TestBuildState_SingleModuleProject(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.test/uistate\n\ngo 1.25\n")
	writeFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")
	writeFile(t, filepath.Join(root, "main_test.go"), "package main\n\nimport \"testing\"\n\nfunc TestMain(t *testing.T) {}\n")

	session := newSessionWithProject(t, root)
	state, err := BuildState(session)
	if err != nil {
		t.Fatalf("BuildState: %v", err)
	}
	if !state.Project.Open {
		t.Fatal("Project.Open = false, want true")
	}
	if state.Project.Root == "" {
		t.Fatal("Project.Root is empty")
	}
	if len(state.Project.Modules) != 1 {
		t.Fatalf("Modules len = %d, want 1", len(state.Project.Modules))
	}
	if len(state.Project.Packages) != 1 {
		t.Fatalf("Packages len = %d, want 1: %+v", len(state.Project.Packages), state.Project.Packages)
	}
	pkg := state.Project.Packages[0]
	if pkg.ImportPath != "example.test/uistate" {
		t.Fatalf("Package.ImportPath = %q", pkg.ImportPath)
	}
	if !hasFile(pkg.Files, "main.go") {
		t.Fatalf("Files missing main.go: %+v", pkg.Files)
	}
	if !hasFile(pkg.TestFiles, "main_test.go") {
		t.Fatalf("TestFiles missing main_test.go: %+v", pkg.TestFiles)
	}
}

func TestBuildState_MultiModuleProjectDeterministicOrdering(t *testing.T) {
	root := t.TempDir()
	modB := filepath.Join(root, "modb")
	modA := filepath.Join(root, "moda")
	writeFile(t, filepath.Join(root, "go.work"), "go 1.25.0\n\nuse (\n\t./modb\n\t./moda\n)\n")
	writeFile(t, filepath.Join(modB, "go.mod"), "module example.test/modb\n\ngo 1.25\n")
	writeFile(t, filepath.Join(modB, "b.go"), "package modb\n\nfunc B() {}\n")
	writeFile(t, filepath.Join(modA, "go.mod"), "module example.test/moda\n\ngo 1.25\n")
	writeFile(t, filepath.Join(modA, "a.go"), "package moda\n\nfunc A() {}\n")

	session := newSessionWithProject(t, root)
	state, err := BuildState(session)
	if err != nil {
		t.Fatalf("BuildState: %v", err)
	}
	if got := moduleRelDirs(state.Project.Modules); len(got) != 2 || got[0] != "moda" || got[1] != "modb" {
		t.Fatalf("module rel dirs = %v, want [moda modb]", got)
	}
	if got := packageImportPaths(state.Project.Packages); len(got) != 2 ||
		got[0] != "example.test/moda" || got[1] != "example.test/modb" {
		t.Fatalf("package import paths = %v, want sorted moda/modb", got)
	}
}

func TestBuildState_ReturnsDefensiveSlices(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.test/defensive\n\ngo 1.25\n")
	writeFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")

	session := newSessionWithProject(t, root)
	if err := session.Dispatcher().Dispatch(context.Background(), app.CommandOpenFile, map[string]any{
		"path": filepath.Join(root, "main.go"),
	}); err != nil {
		t.Fatalf("Dispatch openFile: %v", err)
	}
	state, err := BuildState(session)
	if err != nil {
		t.Fatalf("BuildState: %v", err)
	}
	state.Commands[0].Name = "mutated"
	state.Buffers[0].DisplayName = "mutated"
	state.Project.Modules[0].RelDir = "mutated"
	state.Project.Packages[0].Files[0].RelPath = "mutated.go"

	next, err := BuildState(session)
	if err != nil {
		t.Fatalf("BuildState again: %v", err)
	}
	if next.Commands[0].Name == "mutated" {
		t.Fatal("Commands returned mutable state")
	}
	if next.Buffers[0].DisplayName == "mutated" {
		t.Fatal("Buffers returned mutable state")
	}
	if next.Project.Modules[0].RelDir == "mutated" {
		t.Fatal("Modules returned mutable state")
	}
	if next.Project.Packages[0].Files[0].RelPath == "mutated.go" {
		t.Fatal("Files returned mutable state")
	}
}

func newRegisteredSession(t *testing.T, opts app.Options) *app.Session {
	t.Helper()
	session, err := app.NewSession(opts)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := session.RegisterCommands(); err != nil {
		t.Fatalf("RegisterCommands: %v", err)
	}
	return session
}

func newSessionWithProject(t *testing.T, root string) *app.Session {
	t.Helper()
	p, err := project.Open(context.Background(), root, project.Options{})
	if err != nil {
		t.Fatalf("project.Open: %v", err)
	}
	return newRegisteredSession(t, app.Options{Project: p})
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

func hasFile(files []FileItem, relPath string) bool {
	for _, file := range files {
		if file.RelPath == relPath {
			return true
		}
	}
	return false
}

func moduleRelDirs(modules []ModuleItem) []string {
	out := make([]string, len(modules))
	for i, module := range modules {
		out[i] = module.RelDir
	}
	return out
}

func packageImportPaths(pkgs []PackageItem) []string {
	out := make([]string, len(pkgs))
	for i, pkg := range pkgs {
		out[i] = pkg.ImportPath
	}
	return out
}
