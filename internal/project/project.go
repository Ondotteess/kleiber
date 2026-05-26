package project

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Ondotteess/kleiber/internal/logging"
)

// ErrNoModule is returned by Open when the supplied root directory (and
// none of its ancestors) contains a go.mod or go.work file.
var ErrNoModule = errors.New("project: no Go module found at or above root")

// ErrFileOutsideProject is returned by FileForPath for paths outside the
// project root.
var ErrFileOutsideProject = errors.New("project: file is outside project root")

// ErrFileNotInPackage is returned by FileForPath when the file is inside
// the project root but not part of any loaded package (e.g., a Markdown
// file or a Go file in a directory go/packages did not return).
var ErrFileNotInPackage = errors.New("project: file is not part of any loaded package")

// ErrPackageLoad is returned by Open or Refresh when go/packages reports
// package-level errors. Pre-alpha prefers failing loudly over exposing a
// partially broken package graph as usable.
var ErrPackageLoad = errors.New("project: package load error")

// Project is the in-memory model of an opened Go workspace.
//
// Project is safe for concurrent use. Accessors return snapshots, never
// references to the project's internal state, so callers may mutate the
// returned slices freely.
type Project struct {
	logger *slog.Logger
	root   string

	mu       sync.RWMutex
	modules  []Module
	packages []Package
}

// Module mirrors the subset of go.mod that Kleiber exposes.
type Module struct {
	// Path is the module's import path (e.g., "github.com/Ondotteess/kleiber").
	Path string

	// Dir is the absolute directory that contains the module's go.mod.
	Dir string

	// GoMod is the absolute path to go.mod itself.
	GoMod string

	// GoVersion is the value of the "go" directive (e.g., "1.25") or
	// the empty string if go.mod omits it.
	GoVersion string
}

// Package is a Go package within the project's workspace.
type Package struct {
	// ImportPath is the canonical import path (e.g., "fmt",
	// "github.com/Ondotteess/kleiber/internal/lsp").
	ImportPath string

	// Dir is the absolute directory containing the package's source files.
	Dir string

	// Files lists the absolute paths of the package's non-test .go files.
	Files []string

	// TestFiles lists the absolute paths of the package's *_test.go files.
	TestFiles []string
}

// File describes a Go source file's place in the project.
type File struct {
	Path    string
	Package Package
	IsTest  bool
}

// ProjectSnapshot is a point-in-time copy of project metadata suitable for UI
// state reads. It does not perform I/O and is independent from Project's
// internal slices.
type ProjectSnapshot struct {
	Root     string
	Modules  []Module
	Packages []Package
}

// Options controls Project construction.
type Options struct {
	// Logger receives diagnostic events. Nil means "discard logs".
	Logger *slog.Logger
}

// Open loads the project rooted at root: it locates go.mod (or go.work),
// parses module metadata, and enumerates packages via go/packages. The
// supplied context bounds the load duration; pass a deadline if you want
// to fail fast on a hung `go list` subprocess.
func Open(ctx context.Context, root string, opts Options) (*Project, error) {
	if opts.Logger == nil {
		opts.Logger = logging.Discard()
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolving root %s: %w", root, err)
	}

	p := &Project{
		logger: opts.Logger.With("root", absRoot),
		root:   absRoot,
	}

	modules, packages, err := p.loadSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	p.modules = modules
	p.packages = packages
	p.logger.Info("project opened",
		"modules", len(p.modules),
		"packages", len(p.packages),
	)
	return p, nil
}

// Root returns the absolute project root directory.
func (p *Project) Root() string { return p.root }

// Snapshot returns a defensive copy of the project's current metadata. It is a
// read-only state API for UI/orchestration callers; refreshing stale metadata is
// explicit via Refresh.
func (p *Project) Snapshot() ProjectSnapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()

	modules := make([]Module, len(p.modules))
	copy(modules, p.modules)

	packages := make([]Package, len(p.packages))
	for i, src := range p.packages {
		packages[i] = src.clone()
	}

	return ProjectSnapshot{
		Root:     p.root,
		Modules:  modules,
		Packages: packages,
	}
}

// Refresh reloads module and package metadata from disk and swaps the
// project's snapshots atomically. It is a manual operation: filesystem
// Watch events are deliberately not wired to automatic reloads yet
// because debounce and ownership rules belong in a higher orchestration
// layer.
func (p *Project) Refresh(ctx context.Context) error {
	modules, packages, err := p.loadSnapshot(ctx)
	if err != nil {
		return err
	}

	p.mu.Lock()
	p.modules = modules
	p.packages = packages
	p.mu.Unlock()

	p.logger.Info("project refreshed",
		"modules", len(modules),
		"packages", len(packages),
	)
	return nil
}

func (p *Project) loadSnapshot(ctx context.Context) ([]Module, []Package, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	snapshot := &Project{
		logger: p.logger,
		root:   p.root,
	}
	if err := snapshot.loadModules(ctx); err != nil {
		return nil, nil, err
	}
	if err := snapshot.loadPackages(ctx); err != nil {
		return nil, nil, err
	}
	return snapshot.modules, snapshot.packages, nil
}

// Modules returns a snapshot of the project's modules.
func (p *Project) Modules() []Module {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]Module, len(p.modules))
	copy(out, p.modules)
	return out
}

// Packages returns a snapshot of the project's packages.
func (p *Project) Packages() []Package {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]Package, len(p.packages))
	for i, src := range p.packages {
		out[i] = src.clone()
	}
	return out
}

// FileForPath resolves an absolute or root-relative file path to the
// package that owns it. The file must lie inside the project root and be
// listed in one of the loaded packages.
//
// Common errors:
//   - ErrFileOutsideProject if the path is outside Root().
//   - ErrFileNotInPackage if the path is inside the project but no loaded
//     package claims it.
func (p *Project) FileForPath(path string) (File, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return File{}, fmt.Errorf("resolving %s: %w", path, err)
	}
	rel, err := filepath.Rel(p.root, abs)
	if err != nil {
		return File{}, fmt.Errorf("resolving relative path: %w", err)
	}
	if strings.HasPrefix(rel, "..") {
		return File{}, fmt.Errorf("%w: %s", ErrFileOutsideProject, abs)
	}

	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, pkg := range p.packages {
		for _, f := range pkg.Files {
			if pathsEqual(f, abs) {
				return File{Path: abs, Package: pkg.clone(), IsTest: false}, nil
			}
		}
		for _, f := range pkg.TestFiles {
			if pathsEqual(f, abs) {
				return File{Path: abs, Package: pkg.clone(), IsTest: true}, nil
			}
		}
	}
	return File{}, fmt.Errorf("%w: %s", ErrFileNotInPackage, abs)
}

// clone returns a deep-ish copy of pkg suitable for handing to callers.
func (pkg Package) clone() Package {
	out := Package{
		ImportPath: pkg.ImportPath,
		Dir:        pkg.Dir,
	}
	if len(pkg.Files) > 0 {
		out.Files = make([]string, len(pkg.Files))
		copy(out.Files, pkg.Files)
	}
	if len(pkg.TestFiles) > 0 {
		out.TestFiles = make([]string, len(pkg.TestFiles))
		copy(out.TestFiles, pkg.TestFiles)
	}
	return out
}

// pathsEqual compares two filesystem paths in a case-insensitive way on
// Windows and case-sensitive elsewhere.
func pathsEqual(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if filepath.Separator == '\\' {
		return strings.EqualFold(a, b)
	}
	return a == b
}
