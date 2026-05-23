//go:build integration

package project_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ondotteess/kleiber/internal/project"
)

// helloFixture returns the absolute path to the fixtures/hello module.
// It walks upward from the test working directory looking for
// "fixtures/hello/go.mod", which uniquely identifies the repo root.
func helloFixture(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir, err := filepath.Abs(wd)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	for {
		candidate := filepath.Join(dir, "fixtures", "hello", "go.mod")
		if _, err := os.Stat(candidate); err == nil {
			return filepath.Dir(candidate)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate fixtures/hello from %s", wd)
		}
		dir = parent
	}
}

func TestOpen_HelloFixture_LoadsModuleAndPackages(t *testing.T) {
	root := helloFixture(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	p, err := project.Open(ctx, root, project.Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	mods := p.Modules()
	if len(mods) != 1 {
		t.Fatalf("Modules() len = %d, want 1", len(mods))
	}
	const wantImportPath = "github.com/Ondotteess/kleiber/fixtures/hello"
	if mods[0].Path != wantImportPath {
		t.Errorf("Modules()[0].Path = %q, want %q", mods[0].Path, wantImportPath)
	}

	pkgs := p.Packages()
	if len(pkgs) == 0 {
		t.Fatalf("Packages() returned 0 packages")
	}

	var mainPkg *project.Package
	for i := range pkgs {
		if pkgs[i].ImportPath == wantImportPath {
			mainPkg = &pkgs[i]
			break
		}
	}
	if mainPkg == nil {
		t.Fatalf("did not find package %q in Packages()", wantImportPath)
	}
	if len(mainPkg.Files) == 0 {
		t.Fatalf("main package has no Files")
	}

	var mainFile string
	for _, f := range mainPkg.Files {
		if strings.HasSuffix(f, "main.go") {
			mainFile = f
			break
		}
	}
	if mainFile == "" {
		t.Fatalf("main.go not found among %v", mainPkg.Files)
	}

	file, err := p.FileForPath(mainFile)
	if err != nil {
		t.Fatalf("FileForPath(%s): %v", mainFile, err)
	}
	if file.Package.ImportPath != wantImportPath {
		t.Errorf("FileForPath(...).Package.ImportPath = %q, want %q",
			file.Package.ImportPath, wantImportPath)
	}
	if file.IsTest {
		t.Error("FileForPath for main.go reports IsTest=true")
	}
}
