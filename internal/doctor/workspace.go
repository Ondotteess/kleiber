package doctor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const workspaceCheckName = "workspace"

// WorkspaceCheck walks the project root looking for go.mod files and
// flags multi-module setups that are not accompanied by a go.work
// file. A go.work file tells the Go toolchain (and gopls) that the
// modules under it are developed together — without it, cross-module
// editing relies on `replace` directives or remote versions, which is
// fragile in a monorepo.
type WorkspaceCheck struct{}

// NewWorkspaceCheck constructs the workspace check.
func NewWorkspaceCheck() *WorkspaceCheck { return &WorkspaceCheck{} }

// Name returns the canonical check name.
func (*WorkspaceCheck) Name() string { return workspaceCheckName }

// Run enumerates every go.mod under root (skipping vendored and hidden
// directories) and inspects the presence of go.work at root.
func (c *WorkspaceCheck) Run(ctx context.Context, root string) Finding {
	goMods, err := findAllGoMods(ctx, root)
	if err != nil {
		return Finding{
			CheckName: c.Name(),
			Severity:  SeverityError,
			Title:     "scanning for go.mod files",
			Detail:    err.Error(),
		}
	}

	switch len(goMods) {
	case 0:
		return Finding{
			CheckName: c.Name(),
			Severity:  SeverityInfo,
			Title:     "no go.mod files found below " + root,
			Hint:      "verify this is the correct project root",
		}
	case 1:
		rel, _ := filepath.Rel(root, goMods[0])
		return Finding{
			CheckName: c.Name(),
			Severity:  SeverityOK,
			Title:     "single Go module at " + filepath.ToSlash(rel),
		}
	}

	if _, err := os.Stat(filepath.Join(root, "go.work")); err == nil {
		return Finding{
			CheckName: c.Name(),
			Severity:  SeverityOK,
			Title:     fmt.Sprintf("multi-module workspace (%d modules, go.work present)", len(goMods)),
		}
	}

	rels := make([]string, 0, len(goMods))
	dirs := make([]string, 0, len(goMods))
	for _, gm := range goMods {
		rel, err := filepath.Rel(root, gm)
		if err != nil {
			rel = gm
		}
		rels = append(rels, filepath.ToSlash(rel))
		dir := filepath.ToSlash(filepath.Dir(rel))
		if dir == "." || dir == "" {
			dirs = append(dirs, ".")
		} else {
			dirs = append(dirs, "./"+dir)
		}
	}

	detail := "found go.mod files:\n  - " + strings.Join(rels, "\n  - ")
	return Finding{
		CheckName: c.Name(),
		Severity:  SeverityWarning,
		Title:     fmt.Sprintf("%d Go modules without a go.work", len(goMods)),
		Detail:    detail,
		Hint:      "create a go.work so the modules see each other locally",
		Fixes: []FixAction{{
			Label:   "Initialize go.work",
			Command: "go work init " + strings.Join(dirs, " "),
		}},
	}
}

// findAllGoMods walks root looking for go.mod files. Hidden directories
// (those whose base name starts with ".") and conventional output dirs
// (vendor, node_modules, bin, dist, testdata) are skipped.
func findAllGoMods(ctx context.Context, root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			if shouldSkipDir(path, root, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "go.mod" {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

// shouldSkipDir reports whether walking should descend into a
// directory. The project root itself is never skipped.
func shouldSkipDir(path, root, name string) bool {
	if path == root {
		return false
	}
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "node_modules", "vendor", "bin", "dist", "testdata":
		return true
	}
	return false
}
