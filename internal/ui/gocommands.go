package ui

import (
	"path/filepath"
	"strings"

	"github.com/Ondotteess/kleiber/internal/project"
)

// GoCommand is a named Go toolchain invocation the terminal toolbar can
// run. Args is a full argv (including "go") suitable for terminal.Options.
type GoCommand struct {
	ID    string
	Label string
	Args  []string
}

// GoCommands returns the built-in Go toolchain commands offered by the
// terminal toolbar, in display order: run, build, test, mod tidy.
func GoCommands() []GoCommand {
	return []GoCommand{
		{ID: "run", Label: "Run", Args: []string{"go", "run", "."}},
		{ID: "build", Label: "Build", Args: []string{"go", "build", "./..."}},
		{ID: "test", Label: "Test", Args: []string{"go", "test", "./..."}},
		{ID: "tidy", Label: "Mod Tidy", Args: []string{"go", "mod", "tidy"}},
	}
}

// CommandDir returns the directory a Go command should run in for the
// workbench: the module directory containing the active file when known,
// otherwise the project's first module directory, otherwise the workspace
// root. It never returns empty unless the workbench has no root at all.
func CommandDir(wb *Workbench) string {
	snap, hasProject := wb.Session().ProjectSnapshot()
	if tab, ok := wb.ActiveTab(); ok && hasProject {
		if dir := moduleDirForFile(snap.Modules, tab.Path); dir != "" {
			return dir
		}
	}
	if hasProject && len(snap.Modules) > 0 {
		return snap.Modules[0].Dir
	}
	return wb.Root()
}

// moduleDirForFile returns the directory of the module that most
// specifically contains path (the longest matching module directory), or
// "" when no module contains it.
func moduleDirForFile(modules []project.Module, path string) string {
	best := ""
	for _, m := range modules {
		if m.Dir == "" {
			continue
		}
		if pathWithin(m.Dir, path) && len(m.Dir) > len(best) {
			best = m.Dir
		}
	}
	return best
}

// pathWithin reports whether path is dir itself or lies beneath it,
// comparing case-insensitively on Windows.
func pathWithin(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
