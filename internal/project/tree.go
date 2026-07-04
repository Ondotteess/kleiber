package project

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ErrTreeRoot is returned by BuildFileTree (and Project.FileTree) when the
// tree root itself cannot be read. Unreadable subdirectories, by contrast,
// are kept as childless nodes rather than failing the whole walk.
var ErrTreeRoot = errors.New("project: file tree root is not readable")

// TreeNode is one entry in a hierarchical file tree built by BuildFileTree.
type TreeNode struct {
	// Name is the entry's base name (e.g., "main.go", "internal").
	Name string

	// Path is the entry's absolute filesystem path.
	Path string

	// RelPath is the entry's slash-separated path relative to the tree
	// root (e.g., "internal/project/tree.go"). The root node itself has
	// RelPath ".".
	RelPath string

	// IsDir reports whether the entry is a directory. Symlinks are never
	// followed, so a symlink to a directory has IsDir false.
	IsDir bool

	// Children lists a directory's direct children: directories first,
	// then files, each group sorted case-insensitively by name with ties
	// broken case-sensitively. Nil for files and for directories that
	// are empty or unreadable.
	Children []TreeNode
}

// BuildFileTree walks the real filesystem rooted at root and returns a
// hierarchical tree of every visible entry. All file types are included,
// not only Go sources: an IDE tree must show README, go.mod, assets, and
// so on.
//
// The root node represents root itself: Name is the directory's base name
// and RelPath is ".". Entries whose base name matches the watch skip rules
// (dot-prefixed names such as .git and .gomodcache, plus bin, dist,
// vendor, node_modules) are omitted. Child ordering is deterministic:
// directories first, then files, each group sorted case-insensitively by
// name with ties broken case-sensitively.
//
// Directory symlinks are not followed, avoiding cycles; symlinks appear as
// leaf file nodes. Subdirectories that cannot be read are kept as
// childless nodes so the tree still shows what it can. Only an unreadable
// root fails, with an error matching ErrTreeRoot via errors.Is.
//
// Cancellation is checked between directories; a canceled ctx yields an
// error wrapping ctx.Err(). The returned tree is a plain value owned by
// the caller: it shares no mutable state with this package, so callers may
// keep and mutate it freely.
func BuildFileTree(ctx context.Context, root string) (TreeNode, error) {
	if err := ctx.Err(); err != nil {
		return TreeNode{}, fmt.Errorf("project: building file tree: %w", err)
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return TreeNode{}, fmt.Errorf("project: resolving tree root %s: %w", root, err)
	}

	entries, err := os.ReadDir(absRoot)
	if err != nil {
		return TreeNode{}, fmt.Errorf("%w: %s: %w", ErrTreeRoot, absRoot, err)
	}

	node := TreeNode{
		Name:    filepath.Base(absRoot),
		Path:    absRoot,
		RelPath: ".",
		IsDir:   true,
	}
	node.Children, err = buildTreeChildren(ctx, absRoot, ".", entries)
	if err != nil {
		return TreeNode{}, err
	}
	return node, nil
}

// FileTree builds a file tree of the project root. It is shorthand for
// BuildFileTree(ctx, p.Root()); see BuildFileTree for the skip, sorting,
// symlink, and error semantics. Each call re-walks the filesystem and
// returns a fresh value tree owned by the caller.
func (p *Project) FileTree(ctx context.Context) (TreeNode, error) {
	return BuildFileTree(ctx, p.root)
}

// buildTreeChildren converts one directory's entries into sorted child
// nodes, recursing into readable subdirectories. dir is the directory's
// absolute path and rel its slash-separated path relative to the tree
// root ("." for the root itself).
func buildTreeChildren(ctx context.Context, dir, rel string, entries []os.DirEntry) ([]TreeNode, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("project: building file tree for %s: %w", dir, err)
	}

	var nodes []TreeNode
	for _, entry := range entries {
		name := entry.Name()
		if skipDir(name) {
			continue
		}
		child := TreeNode{
			Name:    name,
			Path:    filepath.Join(dir, name),
			RelPath: treeRel(rel, name),
			// DirEntry types use lstat semantics: a symlink to a
			// directory reports IsDir false, so links become leaf
			// file nodes and are never followed.
			IsDir: entry.IsDir(),
		}
		if child.IsDir {
			subEntries, readErr := os.ReadDir(child.Path)
			if readErr != nil {
				// Unreadable subdirectory: keep the node without
				// children so the tree shows what it can.
				nodes = append(nodes, child)
				continue
			}
			children, err := buildTreeChildren(ctx, child.Path, child.RelPath, subEntries)
			if err != nil {
				return nil, err
			}
			child.Children = children
		}
		nodes = append(nodes, child)
	}
	slices.SortFunc(nodes, compareTreeNodes)
	return nodes, nil
}

// treeRel joins a parent's root-relative slash path with a child name.
func treeRel(parent, name string) string {
	if parent == "." {
		return name
	}
	return parent + "/" + name
}

// compareTreeNodes orders directories before files, then sorts each group
// case-insensitively by name, breaking ties case-sensitively so the order
// is total and deterministic.
func compareTreeNodes(a, b TreeNode) int {
	if a.IsDir != b.IsDir {
		if a.IsDir {
			return -1
		}
		return 1
	}
	if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
		return c
	}
	return strings.Compare(a.Name, b.Name)
}
