package ui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Ondotteess/kleiber/internal/app"
	"github.com/Ondotteess/kleiber/internal/editor"
	"github.com/Ondotteess/kleiber/internal/project"
)

// ErrNoActiveTab is returned by helpers that need an open editor when none
// is active.
var ErrNoActiveTab = errors.New("ui: no active tab")

// Tab is one open editor in the workbench: a buffer, a view onto it, and
// display metadata. Dirty state is queried live from the engine rather
// than cached here, because it changes on every keystroke.
type Tab struct {
	BufferID editor.BufferID
	ViewID   editor.ViewID
	Path     string
	Name     string
}

// Workbench is the stateful IDE model behind the Gio window: the set of
// open editor tabs, the active tab, and the file-tree explorer state. It
// drives editor mutations through the engine's typed API (not the command
// dispatcher, which cannot hand back the new buffer/view handles) and
// imports no gioui packages, so it is unit-testable headless.
//
// Workbench is safe for concurrent use; the render loop reads it while
// background goroutines (tree refresh, LSP) may touch it.
type Workbench struct {
	session *app.Session
	engine  *editor.EditorEngine

	mu       sync.Mutex
	root     string
	tabs     []Tab
	active   int // index into tabs; -1 when empty
	tree     project.TreeNode
	treeOK   bool
	expanded map[string]bool // tree dir RelPath -> expanded
}

// NewWorkbench constructs a Workbench over a session. It performs no I/O;
// call RefreshTree to load the file explorer. When the session has an open
// project, the explorer root defaults to the project root; override it
// with SetRoot.
func NewWorkbench(session *app.Session) (*Workbench, error) {
	if session == nil {
		return nil, ErrNilSession
	}
	root := ""
	if proj := session.Project(); proj != nil {
		root = proj.Root()
	}
	return &Workbench{
		session:  session,
		engine:   session.Editor(),
		root:     root,
		active:   -1,
		expanded: map[string]bool{},
	}, nil
}

// SetRoot sets the file-explorer root directory. It is used when the
// workspace root is known independently of a loaded project — notably so
// the tree still renders for a project whose code does not compile (which
// an IDE must open). Call RefreshTree afterward to reload.
func (w *Workbench) SetRoot(root string) {
	w.mu.Lock()
	w.root = root
	w.mu.Unlock()
}

// Root returns the file-explorer root directory.
func (w *Workbench) Root() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.root
}

// Session returns the underlying app session.
func (w *Workbench) Session() *app.Session { return w.session }

// Engine returns the underlying editor engine.
func (w *Workbench) Engine() *editor.EditorEngine { return w.engine }

// OpenFile opens path in a new tab, or activates the existing tab if the
// file is already open. The file is read into an editor buffer with a
// fresh view. Returns the (possibly pre-existing) tab.
func (w *Workbench) OpenFile(ctx context.Context, path string) (Tab, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Tab{}, err
	}

	w.mu.Lock()
	for i, tab := range w.tabs {
		if pathsEqual(tab.Path, abs) {
			w.active = i
			w.mu.Unlock()
			return tab, nil
		}
	}
	w.mu.Unlock()

	id, err := w.engine.Open(ctx, abs)
	if err != nil {
		return Tab{}, err
	}
	vid, err := w.engine.NewView(id)
	if err != nil {
		return Tab{}, err
	}
	resolved, err := w.engine.Path(id)
	if err != nil {
		resolved = abs
	}
	tab := Tab{
		BufferID: id,
		ViewID:   vid,
		Path:     resolved,
		Name:     filepath.Base(resolved),
	}

	w.mu.Lock()
	w.tabs = append(w.tabs, tab)
	w.active = len(w.tabs) - 1
	w.mu.Unlock()
	return tab, nil
}

// Tabs returns a snapshot of the open tabs in order.
func (w *Workbench) Tabs() []Tab {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]Tab(nil), w.tabs...)
}

// ActiveIndex returns the active tab index, or -1 when no tab is open.
func (w *Workbench) ActiveIndex() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.active
}

// ActiveTab returns the active tab and true, or a zero tab and false.
func (w *Workbench) ActiveTab() (Tab, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.active < 0 || w.active >= len(w.tabs) {
		return Tab{}, false
	}
	return w.tabs[w.active], true
}

// Activate selects the tab at index. Out-of-range indices are ignored.
func (w *Workbench) Activate(index int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if index >= 0 && index < len(w.tabs) {
		w.active = index
	}
}

// CloseTab closes the tab at index, releasing its view and buffer, and
// clamps the active selection to a still-open neighbor.
func (w *Workbench) CloseTab(index int) error {
	w.mu.Lock()
	if index < 0 || index >= len(w.tabs) {
		w.mu.Unlock()
		return nil
	}
	tab := w.tabs[index]
	w.tabs = append(w.tabs[:index], w.tabs[index+1:]...)
	if w.active >= len(w.tabs) {
		w.active = len(w.tabs) - 1
	}
	w.mu.Unlock()

	// Release engine resources outside the lock.
	_ = w.engine.CloseView(tab.ViewID)
	return w.engine.Close(tab.BufferID)
}

// ActiveView returns the *editor.View for the active tab.
func (w *Workbench) ActiveView() (*editor.View, error) {
	tab, ok := w.ActiveTab()
	if !ok {
		return nil, ErrNoActiveTab
	}
	return w.engine.View(tab.ViewID)
}

// ActiveBuffer returns the *editor.Buffer for the active tab.
func (w *Workbench) ActiveBuffer() (*editor.Buffer, error) {
	tab, ok := w.ActiveTab()
	if !ok {
		return nil, ErrNoActiveTab
	}
	return w.engine.Buffer(tab.BufferID)
}

// BufferDirty reports whether the given buffer has unsaved changes.
func (w *Workbench) BufferDirty(id editor.BufferID) bool {
	dirty, err := w.engine.Dirty(id)
	return err == nil && dirty
}

// RefreshTree reloads the file-tree explorer by walking the root
// directory. It builds the tree straight from the filesystem (not from
// loaded project analysis), so the explorer works even when the project's
// Go code does not compile. With no root set it clears the tree.
func (w *Workbench) RefreshTree(ctx context.Context) error {
	root := w.Root()
	if root == "" {
		w.mu.Lock()
		w.tree = project.TreeNode{}
		w.treeOK = false
		w.mu.Unlock()
		return nil
	}
	tree, err := project.BuildFileTree(ctx, root)
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.tree = tree
	w.treeOK = true
	w.mu.Unlock()
	return nil
}

// Tree returns the current file tree and whether it has been loaded.
func (w *Workbench) Tree() (project.TreeNode, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.tree, w.treeOK
}

// ToggleExpand flips the expanded state of a directory node by RelPath.
func (w *Workbench) ToggleExpand(relPath string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.expanded[relPath] = !w.expanded[relPath]
}

// SetExpanded sets the expanded state of a directory node explicitly.
func (w *Workbench) SetExpanded(relPath string, expanded bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.expanded[relPath] = expanded
}

// IsExpanded reports whether a directory node is expanded. The tree root
// is always considered expanded so its children render.
func (w *Workbench) IsExpanded(relPath string) bool {
	if relPath == "." || relPath == "" {
		return true
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.expanded[relPath]
}

// pathsEqual compares two absolute paths, case-insensitively on Windows
// where the filesystem is case-preserving but case-insensitive.
func pathsEqual(a, b string) bool {
	if filepath.Separator == '\\' {
		return strings.EqualFold(a, b)
	}
	return a == b
}
