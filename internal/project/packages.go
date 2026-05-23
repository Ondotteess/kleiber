package project

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// loadPackages populates p.packages by invoking go/packages with the
// "./..." pattern from the first module's directory. Test files are
// included; synthetic test-binary packages produced by Tests=true are
// filtered out so we only expose source-level packages to callers.
func (p *Project) loadPackages(ctx context.Context) error {
	if len(p.modules) == 0 {
		return nil
	}

	cfg := &packages.Config{
		Context: ctx,
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedModule |
			packages.NeedCompiledGoFiles,
		Dir:   p.modules[0].Dir,
		Tests: true,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return fmt.Errorf("loading packages: %w", err)
	}

	// go/packages with Tests=true returns synthetic entries whose ID
	// looks like "foo [foo.test]". Skip those; the source-level
	// "foo" entry already lists every .go file (including _test.go).
	out := make([]Package, 0, len(pkgs))
	seen := map[string]bool{}
	for _, pp := range pkgs {
		if pp == nil || pp.PkgPath == "" {
			continue
		}
		if strings.Contains(pp.ID, " [") {
			continue
		}
		if seen[pp.PkgPath] {
			continue
		}
		seen[pp.PkgPath] = true

		pkg := Package{ImportPath: pp.PkgPath}
		for _, f := range pp.GoFiles {
			if strings.HasSuffix(f, "_test.go") {
				pkg.TestFiles = append(pkg.TestFiles, f)
			} else {
				pkg.Files = append(pkg.Files, f)
			}
		}
		if len(pkg.Files) > 0 {
			pkg.Dir = filepath.Dir(pkg.Files[0])
		} else if len(pkg.TestFiles) > 0 {
			pkg.Dir = filepath.Dir(pkg.TestFiles[0])
		}
		if pkg.Dir == "" {
			// Packages with no Go files (e.g., external "_test"
			// stubs we already filtered) are not useful.
			continue
		}
		out = append(out, pkg)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ImportPath < out[j].ImportPath })
	p.packages = out
	return nil
}
