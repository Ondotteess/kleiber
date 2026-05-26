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
// "./..." pattern from every module directory. Test files are included;
// synthetic test-binary packages produced by Tests=true are filtered out
// so we only expose source-level packages to callers.
func (p *Project) loadPackages(ctx context.Context) error {
	if len(p.modules) == 0 {
		return nil
	}

	out := []Package{}
	seen := map[string]bool{}
	for _, mod := range p.modules {
		if err := ctx.Err(); err != nil {
			return err
		}
		pkgs, err := loadModulePackages(ctx, mod.Dir)
		if err != nil {
			return err
		}
		for _, pkg := range pkgs {
			key := packageKey(pkg)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, pkg)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].ImportPath != out[j].ImportPath {
			return out[i].ImportPath < out[j].ImportPath
		}
		return out[i].Dir < out[j].Dir
	})
	p.packages = out
	return nil
}

func loadModulePackages(ctx context.Context, dir string) ([]Package, error) {
	cfg := &packages.Config{
		Context: ctx,
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedModule |
			packages.NeedCompiledGoFiles |
			packages.NeedSyntax,
		Dir:   dir,
		Tests: true,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("loading packages in %s: %w", dir, err)
	}
	if err := checkPackageErrors(dir, pkgs); err != nil {
		return nil, err
	}

	out := make([]Package, 0, len(pkgs))
	testFilesByKey := map[string][]string{}
	for _, pp := range pkgs {
		pkg, ok := sourcePackage(pp)
		if ok {
			out = append(out, pkg)
		}
		for key, files := range testFilesForSourcePackage(pp) {
			testFilesByKey[key] = appendUniqueStrings(testFilesByKey[key], files...)
		}
	}
	for i := range out {
		key := packageKey(out[i])
		if files := testFilesByKey[key]; len(files) > 0 {
			out[i].TestFiles = appendUniqueStrings(out[i].TestFiles, files...)
			sort.Strings(out[i].TestFiles)
		}
	}
	return out, nil
}

func checkPackageErrors(dir string, pkgs []*packages.Package) error {
	var details []string
	for _, pp := range pkgs {
		if pp == nil || len(pp.Errors) == 0 {
			continue
		}
		pkgID := pp.ID
		if pkgID == "" {
			pkgID = pp.PkgPath
		}
		if pkgID == "" {
			pkgID = "<unknown>"
		}
		for _, pkgErr := range pp.Errors {
			msg := pkgErr.Msg
			if pkgErr.Pos != "" {
				msg = pkgErr.Pos + ": " + msg
			}
			details = append(details, pkgID+": "+msg)
		}
	}
	if len(details) == 0 {
		return nil
	}
	sort.Strings(details)
	return fmt.Errorf("%w in %s: %s", ErrPackageLoad, dir, strings.Join(details, "; "))
}

func sourcePackage(pp *packages.Package) (Package, bool) {
	// go/packages with Tests=true returns synthetic entries whose ID
	// looks like "foo [foo.test]". Skip those; the source-level
	// "foo" entry already lists every .go file (including _test.go).
	if pp == nil || pp.PkgPath == "" {
		return Package{}, false
	}
	if strings.Contains(pp.ID, " [") {
		return Package{}, false
	}

	pkg := Package{ImportPath: pp.PkgPath}
	for _, f := range pp.GoFiles {
		if !sourceFileInDir(f, pp.Dir) {
			continue
		}
		if strings.HasSuffix(f, "_test.go") {
			pkg.TestFiles = append(pkg.TestFiles, f)
		} else {
			pkg.Files = append(pkg.Files, f)
		}
	}
	sort.Strings(pkg.Files)
	sort.Strings(pkg.TestFiles)

	if len(pkg.Files) > 0 {
		pkg.Dir = filepath.Dir(pkg.Files[0])
	} else if len(pkg.TestFiles) > 0 {
		pkg.Dir = filepath.Dir(pkg.TestFiles[0])
	}
	if pkg.Dir == "" {
		// Packages with no Go files (e.g., external "_test" stubs we
		// already filtered) are not useful.
		return Package{}, false
	}
	return pkg, true
}

func packageKey(pkg Package) string {
	return pkg.ImportPath + "\x00" + filepath.Clean(pkg.Dir)
}

func testFilesForSourcePackage(pp *packages.Package) map[string][]string {
	if pp == nil || (!strings.Contains(pp.ID, " [") && pp.ForTest == "") {
		return nil
	}
	importPath := pp.ForTest
	if importPath == "" {
		importPath = pp.PkgPath
	}
	out := map[string][]string{}
	for _, f := range append(append([]string{}, pp.GoFiles...), pp.CompiledGoFiles...) {
		if !sourceFileInDir(f, pp.Dir) {
			continue
		}
		if !strings.HasSuffix(f, "_test.go") {
			continue
		}
		key := importPath + "\x00" + filepath.Clean(filepath.Dir(f))
		out[key] = appendUniqueStrings(out[key], f)
	}
	return out
}

func sourceFileInDir(path, dir string) bool {
	if dir == "" {
		return true
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != "." && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func appendUniqueStrings(dst []string, values ...string) []string {
	seen := make(map[string]bool, len(dst)+len(values))
	for _, value := range dst {
		seen[value] = true
	}
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		dst = append(dst, value)
	}
	return dst
}
