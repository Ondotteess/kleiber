//go:build gio

package ui

import (
	"context"
	"image"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// treeIndentStepDp is the horizontal indent applied per tree depth level.
const treeIndentStepDp = 14

// IDETree renders the file explorer as a scrolling list of the workbench's
// currently visible rows. Directories expand and collapse on click; files
// open in a tab. It keeps one Clickable per row RelPath, created lazily.
type IDETree struct {
	list   widget.List
	clicks map[string]*widget.Clickable
}

// NewIDETree constructs an empty tree panel.
func NewIDETree() *IDETree {
	return &IDETree{
		list:   widget.List{List: layout.List{Axis: layout.Vertical}},
		clicks: map[string]*widget.Clickable{},
	}
}

// Layout draws the explorer header and the visible tree rows, handling clicks
// against wb. It reports whether a click changed workbench state so the
// caller can invalidate the window.
func (t *IDETree) Layout(gtx layout.Context, th *IDETheme, wb *Workbench) (layout.Dimensions, bool) {
	paint.FillShape(gtx.Ops, th.Panel, clip.Rect{Max: gtx.Constraints.Max}.Op())

	rows := wb.VisibleRows()
	dirtyPaths := t.dirtyPaths(wb)
	changed := false

	dims := layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return t.header(gtx, th)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return material.List(th.Theme, &t.list).Layout(gtx, len(rows), func(gtx layout.Context, index int) layout.Dimensions {
				if t.row(gtx, th, wb, rows[index], dirtyPaths) {
					changed = true
				}
				return layout.Dimensions{Size: image.Point{
					X: gtx.Constraints.Max.X,
					Y: th.LineHeight(gtx),
				}}
			})
		}),
	)
	return dims, changed
}

// header draws the muted "EXPLORER" caption.
func (t *IDETree) header(gtx layout.Context, th *IDETheme) layout.Dimensions {
	return layout.Inset{
		Top:    unit.Dp(6),
		Bottom: unit.Dp(6),
		Left:   unit.Dp(10),
	}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		label := material.Caption(th.Theme, "EXPLORER")
		label.Color = th.Muted
		return label.Layout(gtx)
	})
}

// row draws one tree row inside its Clickable and dispatches a click to the
// workbench. It returns true when the click mutated state.
func (t *IDETree) row(gtx layout.Context, th *IDETheme, wb *Workbench, row TreeRow, dirty map[string]bool) bool {
	click := t.clickFor(row.RelPath)
	changed := false
	if click.Clicked(gtx) {
		if row.IsDir {
			wb.ToggleExpand(row.RelPath)
		} else {
			// A read-only skeleton: a failed open simply leaves state
			// unchanged. Errors surface in a later input-handling task.
			_, _ = wb.OpenFile(context.Background(), row.Path)
		}
		changed = true
	}

	click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		height := th.LineHeight(gtx)
		size := image.Point{X: gtx.Constraints.Max.X, Y: height}
		indent := gtx.Metric.Dp(unit.Dp(treeIndentStepDp))*row.Depth + gtx.Metric.Dp(unit.Dp(8))

		return layout.Inset{Left: pxToDp(gtx, indent)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			label := material.Body2(th.Theme, treeRowText(row, dirty))
			label.Font = th.MonoFont()
			label.MaxLines = 1
			if row.IsDir {
				label.Color = th.Foreground
			} else {
				label.Color = th.Muted
			}
			label.Layout(gtx)
			return layout.Dimensions{Size: size}
		})
	})
	return changed
}

// clickFor returns the Clickable for relPath, creating it on first use.
func (t *IDETree) clickFor(relPath string) *widget.Clickable {
	if c, ok := t.clicks[relPath]; ok {
		return c
	}
	c := &widget.Clickable{}
	t.clicks[relPath] = c
	return c
}

// dirtyPaths builds a set of open-tab paths whose buffers are dirty, so file
// rows can show an unsaved marker without an inner loop per row.
func (t *IDETree) dirtyPaths(wb *Workbench) map[string]bool {
	tabs := wb.Tabs()
	out := make(map[string]bool, len(tabs))
	for _, tab := range tabs {
		if wb.BufferDirty(tab.BufferID) {
			out[tab.Path] = true
		}
	}
	return out
}

// treeRowText composes the row label: a collapse/expand indicator for
// directories, the name, and a dirty marker for modified open files.
func treeRowText(row TreeRow, dirty map[string]bool) string {
	prefix := "  "
	if row.IsDir {
		if row.Expanded {
			prefix = "v "
		} else {
			prefix = "> "
		}
	}
	text := prefix + row.Name
	if !row.IsDir && dirty[row.Path] {
		text += " *"
	}
	return text
}

// pxToDp converts a pixel length back to Dp for use with layout.Inset, which
// takes device-independent units.
func pxToDp(gtx layout.Context, px int) unit.Dp {
	if gtx.Metric.PxPerDp <= 0 {
		return unit.Dp(px)
	}
	return unit.Dp(float32(px) / gtx.Metric.PxPerDp)
}
