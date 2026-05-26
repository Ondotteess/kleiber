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

func TestOpen_GoWork_LoadsPackagesFromAllModules(t *testing.T) {
	root := t.TempDir()
	modA := filepath.Join(root, "moda")
	modB := filepath.Join(root, "modb")
	if err := os.MkdirAll(modA, 0o755); err != nil {
		t.Fatalf("MkdirAll modA: %v", err)
	}
	if err := os.MkdirAll(modB, 0o755); err != nil {
		t.Fatalf("MkdirAll modB: %v", err)
	}

	writeFile(t, filepath.Join(root, "go.work"), "go 1.25.0\n\nuse (\n\t./moda\n\t./modb\n)\n")
	writeFile(t, filepath.Join(modA, "go.mod"), "module example.test/moda\n\ngo 1.25\n")
	writeFile(t, filepath.Join(modA, "a.go"), "package moda\n\nfunc A() {}\n")
	writeFile(t, filepath.Join(modB, "go.mod"), "module example.test/modb\n\ngo 1.25\n")
	modBFile := filepath.Join(modB, "b.go")
	writeFile(t, modBFile, "package modb\n\nfunc B() {}\n")

	p, err := Open(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	mods := p.Modules()
	if len(mods) != 2 {
		t.Fatalf("Modules() len = %d, want 2", len(mods))
	}
	if mods[0].Path != "example.test/moda" || mods[1].Path != "example.test/modb" {
		t.Fatalf("Modules() = %+v, want moda then modb", mods)
	}

	if !hasPackage(p.Packages(), "example.test/moda") {
		t.Fatalf("Packages() missing example.test/moda: %+v", p.Packages())
	}
	if !hasPackage(p.Packages(), "example.test/modb") {
		t.Fatalf("Packages() missing example.test/modb: %+v", p.Packages())
	}

	file, err := p.FileForPath(modBFile)
	if err != nil {
		t.Fatalf("FileForPath(second module file): %v", err)
	}
	if file.Package.ImportPath != "example.test/modb" {
		t.Fatalf("FileForPath package = %q, want example.test/modb", file.Package.ImportPath)
	}
}

func TestOpen_PackageErrors_ReturnsError(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.test/broken\n\ngo 1.25\n")
	writeFile(t, filepath.Join(root, "broken.go"), "package broken\n\nfunc broken( {\n")

	_, err := Open(context.Background(), root, Options{})
	if !errors.Is(err, ErrPackageLoad) {
		t.Fatalf("Open err = %v, want ErrPackageLoad", err)
	}
	if !strings.Contains(err.Error(), "package load error") {
		t.Fatalf("Open err = %v, want package load error detail", err)
	}
}

func TestProject_Refresh_LoadsNewPackage(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.test/refresh\n\ngo 1.25\n")
	writeFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")

	p, err := Open(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if hasPackage(p.Packages(), "example.test/refresh/pkg/extra") {
		t.Fatal("new package visible before it exists")
	}

	extraDir := filepath.Join(root, "pkg", "extra")
	if err := os.MkdirAll(extraDir, 0o755); err != nil {
		t.Fatalf("MkdirAll extraDir: %v", err)
	}
	writeFile(t, filepath.Join(extraDir, "extra.go"), "package extra\n\nfunc Extra() {}\n")

	if err := p.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if !hasPackage(p.Packages(), "example.test/refresh/pkg/extra") {
		t.Fatalf("Packages() missing new package after Refresh: %+v", p.Packages())
	}
}

func TestProject_Refresh_FileForPathSeesNewTestFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.test/tests\n\ngo 1.25\n")
	writeFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")

	p, err := Open(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	testFile := filepath.Join(root, "main_test.go")
	writeFile(t, testFile, "package main\n\nimport \"testing\"\n\nfunc TestMainPackage(t *testing.T) {}\n")
	if _, err := p.FileForPath(testFile); !errors.Is(err, ErrFileNotInPackage) {
		t.Fatalf("FileForPath before Refresh err = %v, want ErrFileNotInPackage", err)
	}

	if err := p.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	file, err := p.FileForPath(testFile)
	if err != nil {
		t.Fatalf("FileForPath after Refresh: %v", err)
	}
	if !file.IsTest {
		t.Fatal("FileForPath IsTest = false, want true")
	}
	if file.Package.ImportPath != "example.test/tests" {
		t.Fatalf("FileForPath package = %q, want example.test/tests", file.Package.ImportPath)
	}
}

func TestProject_Refresh_ContextCanceled(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.test/cancel\n\ngo 1.25\n")
	writeFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")

	p, err := Open(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.Refresh(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Refresh err = %v, want context.Canceled", err)
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

func hasPackage(pkgs []Package, importPath string) bool {
	for _, pkg := range pkgs {
		if pkg.ImportPath == importPath {
			return true
		}
	}
	return false
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
