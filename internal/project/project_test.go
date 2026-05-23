package project

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fsnotify/fsnotify"
)

func TestOpen_NoModule_Error(t *testing.T) {
	dir := t.TempDir()
	_, err := Open(context.Background(), dir, Options{})
	if !errors.Is(err, ErrNoModule) {
		t.Errorf("err = %v, want ErrNoModule", err)
	}
}

func TestOpen_GoMod_LoadsModule(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"),
		"module example.test/widget\n\ngo 1.25\n")
	writeFile(t, filepath.Join(dir, "main.go"),
		"package main\n\nfunc main() {}\n")

	p, err := Open(context.Background(), dir, Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	mods := p.Modules()
	if len(mods) != 1 {
		t.Fatalf("Modules() len = %d, want 1", len(mods))
	}
	if mods[0].Path != "example.test/widget" {
		t.Errorf("Modules()[0].Path = %q, want %q", mods[0].Path, "example.test/widget")
	}
	if mods[0].GoVersion != "1.25" {
		t.Errorf("Modules()[0].GoVersion = %q, want %q", mods[0].GoVersion, "1.25")
	}
}

func TestParseModFile_GoMissing(t *testing.T) {
	dir := t.TempDir()
	goMod := filepath.Join(dir, "go.mod")
	writeFile(t, goMod, "module no.go.directive\n")
	m, err := parseModFile(goMod)
	if err != nil {
		t.Fatalf("parseModFile: %v", err)
	}
	if m.Path != "no.go.directive" {
		t.Errorf("Path = %q", m.Path)
	}
	if m.GoVersion != "" {
		t.Errorf("GoVersion = %q, want empty", m.GoVersion)
	}
}

func TestSkipDir(t *testing.T) {
	cases := map[string]bool{
		".git":         true,
		".kleiber":     true,
		"bin":          true,
		"dist":         true,
		"node_modules": true,
		"vendor":       true,
		"src":          false,
		"internal":     false,
		"a.b":          false,
	}
	for name, want := range cases {
		if got := skipDir(name); got != want {
			t.Errorf("skipDir(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestMapOp(t *testing.T) {
	cases := []struct {
		op   fsnotify.Op
		want FSEventKind
	}{
		{fsnotify.Create, FSEventCreate},
		{fsnotify.Write, FSEventWrite},
		{fsnotify.Remove, FSEventRemove},
		{fsnotify.Rename, FSEventRename},
		{fsnotify.Chmod, FSEventChmod},
		{0, FSEventUnknown},
	}
	for _, tc := range cases {
		if got := mapOp(tc.op); got != tc.want {
			t.Errorf("mapOp(%v) = %v, want %v", tc.op, got, tc.want)
		}
	}
}

func TestFSEventKind_String(t *testing.T) {
	cases := map[FSEventKind]string{
		FSEventCreate:  "create",
		FSEventWrite:   "write",
		FSEventRemove:  "remove",
		FSEventRename:  "rename",
		FSEventChmod:   "chmod",
		FSEventUnknown: "unknown",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("%v.String() = %q, want %q", k, got, want)
		}
	}
}

func TestFileForPath_OutsideRoot_Error(t *testing.T) {
	root := t.TempDir()
	p := &Project{root: root}

	outside := filepath.Join(t.TempDir(), "x.go") // different temp root
	_, err := p.FileForPath(outside)
	if !errors.Is(err, ErrFileOutsideProject) {
		t.Errorf("err = %v, want ErrFileOutsideProject", err)
	}
}

func TestFileForPath_InsideRoot_NoPackage_Error(t *testing.T) {
	root := t.TempDir()
	p := &Project{root: root}
	inside := filepath.Join(root, "lonely.go")
	writeFile(t, inside, "package main\n")
	_, err := p.FileForPath(inside)
	if !errors.Is(err, ErrFileNotInPackage) {
		t.Errorf("err = %v, want ErrFileNotInPackage", err)
	}
}

func TestFileForPath_FindsTestFile(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "foo.go")
	test := filepath.Join(root, "foo_test.go")
	writeFile(t, src, "package x\n")
	writeFile(t, test, "package x\n")

	p := &Project{
		root: root,
		packages: []Package{{
			ImportPath: "example.test/x",
			Dir:        root,
			Files:      []string{src},
			TestFiles:  []string{test},
		}},
	}

	f, err := p.FileForPath(test)
	if err != nil {
		t.Fatalf("FileForPath: %v", err)
	}
	if !f.IsTest {
		t.Error("IsTest = false, want true")
	}
	if f.Package.ImportPath != "example.test/x" {
		t.Errorf("Package.ImportPath = %q", f.Package.ImportPath)
	}
}

func TestPackagesSnapshot_IndependentMutation(t *testing.T) {
	root := t.TempDir()
	p := &Project{
		root: root,
		packages: []Package{{
			ImportPath: "x",
			Dir:        root,
			Files:      []string{filepath.Join(root, "a.go")},
		}},
	}
	snap := p.Packages()
	snap[0].Files[0] = "mutated"

	// Internal state must be unaffected.
	if got := p.packages[0].Files[0]; strings.HasSuffix(got, "mutated") || got == "mutated" {
		t.Errorf("internal state mutated via snapshot: %q", got)
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
