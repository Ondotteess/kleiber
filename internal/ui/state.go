package ui

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Ondotteess/kleiber/internal/app"
	"github.com/Ondotteess/kleiber/internal/editor"
	"github.com/Ondotteess/kleiber/internal/project"
)

// ErrNilSession is returned when BuildState is called without a core session.
var ErrNilSession = errors.New("ui: session is nil")

// State is the pure read model a future renderer can consume. It contains no
// executable commands, no core pointers, and no gioui-specific values.
type State struct {
	Commands []CommandItem
	Buffers  []BufferItem
	Views    []ViewItem
	Project  ProjectState
}

// CommandItem is a command-palette row.
type CommandItem struct {
	Name        string
	Description string
}

// BufferItem is a file/buffer list row.
type BufferItem struct {
	ID          editor.BufferID
	Path        string
	DisplayName string
	Dirty       bool
}

// ViewItem is a read model for one editor view.
type ViewItem struct {
	ID        editor.ViewID
	BufferID  editor.BufferID
	Selection editor.Selection
}

// ProjectState is a lightweight project explorer model. It is intentionally
// not an interactive tree: expand/collapse, icons, selection, and rendering are
// future UI concerns.
type ProjectState struct {
	Open     bool
	Root     string
	Modules  []ModuleItem
	Packages []PackageItem
}

// ModuleItem is a UI-ready module row.
type ModuleItem struct {
	Path      string
	Dir       string
	RelDir    string
	GoMod     string
	GoVersion string
}

// PackageItem groups files by Go package.
type PackageItem struct {
	ImportPath string
	Dir        string
	RelDir     string
	Files      []FileItem
	TestFiles  []FileItem
}

// FileItem is one source file in a package.
type FileItem struct {
	Path        string
	RelPath     string
	DisplayName string
	IsTest      bool
}

// BuildState converts app.Session snapshots into a pure UI read model. It does
// not perform I/O and does not mutate the session or core packages.
func BuildState(session *app.Session) (State, error) {
	if session == nil {
		return State{}, ErrNilSession
	}

	state := State{
		Commands: buildCommands(session.CommandPalette()),
		Buffers:  buildBuffers(session.Buffers()),
		Views:    buildViews(session.Views(0)),
	}
	if snap, ok := session.ProjectSnapshot(); ok {
		state.Project = buildProject(snap)
	}
	return cloneState(state), nil
}

func buildCommands(commands []app.CommandDescriptor) []CommandItem {
	out := make([]CommandItem, len(commands))
	for i, cmd := range commands {
		out[i] = CommandItem{Name: cmd.Name, Description: cmd.Description}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func buildBuffers(buffers []editor.BufferRef) []BufferItem {
	out := make([]BufferItem, len(buffers))
	for i, ref := range buffers {
		out[i] = BufferItem{
			ID:          ref.ID,
			Path:        ref.Path,
			DisplayName: bufferDisplayName(ref),
			Dirty:       ref.Dirty,
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func bufferDisplayName(ref editor.BufferRef) string {
	if ref.Path == "" {
		return "Untitled"
	}
	return filepath.Base(ref.Path)
}

func buildViews(views []editor.ViewRef) []ViewItem {
	out := make([]ViewItem, len(views))
	for i, ref := range views {
		out[i] = ViewItem{
			ID:        ref.ID,
			BufferID:  ref.BufferID,
			Selection: ref.Selection,
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func buildProject(snap project.ProjectSnapshot) ProjectState {
	state := ProjectState{
		Open:     true,
		Root:     snap.Root,
		Modules:  make([]ModuleItem, len(snap.Modules)),
		Packages: make([]PackageItem, len(snap.Packages)),
	}
	for i, module := range snap.Modules {
		state.Modules[i] = ModuleItem{
			Path:      module.Path,
			Dir:       module.Dir,
			RelDir:    relPath(snap.Root, module.Dir),
			GoMod:     module.GoMod,
			GoVersion: module.GoVersion,
		}
	}
	for i, pkg := range snap.Packages {
		state.Packages[i] = PackageItem{
			ImportPath: pkg.ImportPath,
			Dir:        pkg.Dir,
			RelDir:     relPath(snap.Root, pkg.Dir),
			Files:      buildFiles(snap.Root, pkg.Files, false),
			TestFiles:  buildFiles(snap.Root, pkg.TestFiles, true),
		}
	}
	sort.Slice(state.Modules, func(i, j int) bool {
		if state.Modules[i].RelDir == state.Modules[j].RelDir {
			return state.Modules[i].Path < state.Modules[j].Path
		}
		return state.Modules[i].RelDir < state.Modules[j].RelDir
	})
	sort.Slice(state.Packages, func(i, j int) bool {
		if state.Packages[i].ImportPath == state.Packages[j].ImportPath {
			return state.Packages[i].RelDir < state.Packages[j].RelDir
		}
		return state.Packages[i].ImportPath < state.Packages[j].ImportPath
	})
	return state
}

func buildFiles(root string, files []string, isTest bool) []FileItem {
	out := make([]FileItem, len(files))
	for i, path := range files {
		out[i] = FileItem{
			Path:        path,
			RelPath:     relPath(root, path),
			DisplayName: filepath.Base(path),
			IsTest:      isTest,
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RelPath < out[j].RelPath })
	return out
}

func relPath(root, path string) string {
	if path == "" {
		return ""
	}
	if root == "" {
		return filepath.ToSlash(filepath.Clean(path))
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "" || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(filepath.Clean(path))
	}
	return filepath.ToSlash(rel)
}
