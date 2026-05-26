package project

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Ondotteess/kleiber/internal/commands"
)

func TestProject_Snapshot_DefensiveCopies(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.go")
	test := filepath.Join(root, "a_test.go")
	p := &Project{
		root: root,
		modules: []Module{{
			Path:      "example.test/snap",
			Dir:       root,
			GoMod:     filepath.Join(root, "go.mod"),
			GoVersion: "1.25",
		}},
		packages: []Package{{
			ImportPath: "example.test/snap",
			Dir:        root,
			Files:      []string{src},
			TestFiles:  []string{test},
		}},
	}

	snap := p.Snapshot()
	if snap.Root != root {
		t.Fatalf("Snapshot.Root = %q, want %q", snap.Root, root)
	}
	snap.Modules[0].Path = "mutated"
	snap.Packages[0].ImportPath = "mutated"
	snap.Packages[0].Files[0] = "mutated.go"
	snap.Packages[0].TestFiles[0] = "mutated_test.go"

	next := p.Snapshot()
	if next.Modules[0].Path != "example.test/snap" {
		t.Errorf("module path mutated through snapshot: %q", next.Modules[0].Path)
	}
	if next.Packages[0].ImportPath != "example.test/snap" {
		t.Errorf("package path mutated through snapshot: %q", next.Packages[0].ImportPath)
	}
	if next.Packages[0].Files[0] != src {
		t.Errorf("source file mutated through snapshot: %q", next.Packages[0].Files[0])
	}
	if next.Packages[0].TestFiles[0] != test {
		t.Errorf("test file mutated through snapshot: %q", next.Packages[0].TestFiles[0])
	}
}

func TestRegisterCommands_AddsRefreshCommand(t *testing.T) {
	d := commands.New(nil)
	p := &Project{root: t.TempDir()}

	if err := RegisterCommands(d, p); err != nil {
		t.Fatalf("RegisterCommands: %v", err)
	}
	if !d.Has(CommandRefresh) {
		t.Errorf("dispatcher missing %s", CommandRefresh)
	}
}

func TestRegisterCommands_NilInputsError(t *testing.T) {
	if err := RegisterCommands(nil, nil); !errors.Is(err, ErrCommandDispatcherNil) {
		t.Errorf("nil dispatcher err = %v, want ErrCommandDispatcherNil", err)
	}
	d := commands.New(nil)
	if err := RegisterCommands(d, nil); !errors.Is(err, ErrCommandProjectNil) {
		t.Errorf("nil project err = %v, want ErrCommandProjectNil", err)
	}
}

func TestProjectCommands_RefreshUpdatesSnapshot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.test/cmdrefresh\n\ngo 1.25\n")
	writeFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")

	p, err := Open(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	d := commands.New(nil)
	if err := RegisterCommands(d, p); err != nil {
		t.Fatalf("RegisterCommands: %v", err)
	}
	if hasPackage(p.Snapshot().Packages, "example.test/cmdrefresh/pkg/extra") {
		t.Fatal("new package visible before command refresh")
	}

	extraDir := filepath.Join(root, "pkg", "extra")
	if err := os.MkdirAll(extraDir, 0o755); err != nil {
		t.Fatalf("MkdirAll extraDir: %v", err)
	}
	writeFile(t, filepath.Join(extraDir, "extra.go"), "package extra\n\nfunc Extra() {}\n")

	if err := d.Dispatch(context.Background(), CommandRefresh, nil); err != nil {
		t.Fatalf("Dispatch project.refresh: %v", err)
	}
	if !hasPackage(p.Snapshot().Packages, "example.test/cmdrefresh/pkg/extra") {
		t.Fatalf("Snapshot missing new package after refresh: %+v", p.Snapshot().Packages)
	}
}
