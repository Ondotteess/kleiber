package ui

import "github.com/Ondotteess/kleiber/internal/project"

// TreeRow is one visible row of the flattened file explorer. Depth drives
// indentation; Expanded is meaningful only for directories.
type TreeRow struct {
	Name     string
	Path     string
	RelPath  string
	IsDir    bool
	Depth    int
	Expanded bool
}

// VisibleRows flattens the loaded file tree into the rows currently
// visible given the workbench's expansion state, depth-first. The root is
// row zero (always expanded); a directory's children appear only when it
// is expanded. It returns nil when no tree is loaded.
func (w *Workbench) VisibleRows() []TreeRow {
	tree, ok := w.Tree()
	if !ok {
		return nil
	}
	var rows []TreeRow
	w.appendRows(&rows, tree, 0)
	return rows
}

func (w *Workbench) appendRows(rows *[]TreeRow, node project.TreeNode, depth int) {
	expanded := w.IsExpanded(node.RelPath)
	*rows = append(*rows, TreeRow{
		Name:     node.Name,
		Path:     node.Path,
		RelPath:  node.RelPath,
		IsDir:    node.IsDir,
		Depth:    depth,
		Expanded: expanded,
	})
	if !node.IsDir || !expanded {
		return
	}
	for _, child := range node.Children {
		w.appendRows(rows, child, depth+1)
	}
}
