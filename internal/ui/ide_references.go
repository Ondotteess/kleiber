//go:build gio

package ui

import (
	"image"
	"os"
	"path/filepath"
	"strings"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/Ondotteess/kleiber/internal/lsp"
)

// referencesPanelHeightDp is the fixed height of the references panel when
// open, matching the problems panel.
const referencesPanelHeightDp = 160

// referencePreviewMaxFileBytes caps how large a file may be before the panel
// skips reading it for row previews. Larger files show no preview rather than
// stalling a frame on a multi-megabyte read.
const referencePreviewMaxFileBytes = 1 << 20

// referenceJump names the location a references-panel row click asked the
// window to navigate to.
type referenceJump struct {
	// Loc is the clicked reference location; its Path may name a file that is
	// not open yet, in which case the window opens it first.
	Loc lsp.EditorLocation
}

// IDEReferences is the collapsible panel listing the result of the last
// find-references request (Shift+F12), one clickable row per location.
// Clicking a row asks the window to jump there; the panel stays open for
// further browsing. It owns the scroll state, a per-row clickable slice, the
// displayed locations, and a per-population preview cache: files other than
// the active buffer are read from disk at most once per setLocations call.
type IDEReferences struct {
	list widget.List
	rows []*widget.Clickable

	// locations is the last stored reference result, in server order.
	locations []lsp.EditorLocation
	// displays holds the precomputed "path:line:col" label per row, with the
	// path shortened relative to the workbench root when possible.
	displays []string
	// previews caches the split lines of every non-active file a preview was
	// requested from since the last setLocations. A present nil entry records
	// a file that could not (or should not) be read, so it is not retried
	// every frame. Keyed by the location's absolute path.
	previews map[string][]string
}

// NewIDEReferences constructs an empty references panel with a vertical
// scroll list.
func NewIDEReferences() *IDEReferences {
	return &IDEReferences{
		list: widget.List{List: layout.List{Axis: layout.Vertical}},
	}
}

// setLocations replaces the panel's contents with a fresh reference result.
// It precomputes the row labels against the current workbench root, resets
// the preview cache (previews are then re-read lazily as rows become
// visible), and rewinds the scroll position so the first result is visible.
func (r *IDEReferences) setLocations(wb *Workbench, locs []lsp.EditorLocation) {
	root := wb.Root()
	r.locations = locs
	r.displays = make([]string, len(locs))
	for i, loc := range locs {
		r.displays[i] = referenceDisplayPath(root, loc.Path) +
			":" + itoa(loc.Range.Start.Line+1) +
			":" + itoa(loc.Range.Start.Column+1)
	}
	r.previews = map[string][]string{}
	r.list.Position.First = 0
	r.list.Position.Offset = 0
}

// Layout draws the references panel and returns the location a row click
// requested, or nil when nothing was clicked. The panel fills a fixed-height
// region: a header row over a scrolling location list.
func (r *IDEReferences) Layout(gtx layout.Context, th *IDETheme, wb *Workbench) (layout.Dimensions, *referenceJump) {
	height := gtx.Metric.Dp(unit.Dp(referencesPanelHeightDp))
	width := gtx.Constraints.Max.X
	paint.FillShape(gtx.Ops, th.Panel, clip.Rect{Max: image.Point{X: width, Y: height}}.Op())
	// Top divider separating the panel from the region above it.
	paint.FillShape(gtx.Ops, th.Divider, clip.Rect{Max: image.Point{X: width, Y: 1}}.Op())

	r.ensure(len(r.locations))

	var jump *referenceJump

	gtx.Constraints.Min = image.Point{X: width, Y: height}
	gtx.Constraints.Max.Y = height
	layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return r.header(gtx, th, len(r.locations))
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if len(r.locations) == 0 {
				return r.empty(gtx, th)
			}
			return material.List(th.Theme, &r.list).Layout(gtx, len(r.locations), func(gtx layout.Context, index int) layout.Dimensions {
				if r.rows[index].Clicked(gtx) {
					jump = &referenceJump{Loc: r.locations[index]}
				}
				return r.row(gtx, th, wb, index)
			})
		}),
	)

	return layout.Dimensions{Size: image.Point{X: width, Y: height}}, jump
}

// header draws the "REFERENCES" title with a trailing count.
func (r *IDEReferences) header(gtx layout.Context, th *IDETheme, count int) layout.Dimensions {
	return layout.Inset{Left: unit.Dp(10), Right: unit.Dp(10), Top: unit.Dp(5), Bottom: unit.Dp(5)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			text := "REFERENCES"
			if count > 0 {
				text += " (" + itoa(count) + ")"
			}
			label := material.Label(th.Theme, th.Theme.TextSize, text)
			label.Font = th.MonoFont()
			label.Color = th.Muted
			label.MaxLines = 1
			return label.Layout(gtx)
		})
}

// empty renders a muted placeholder when the panel holds no locations.
func (r *IDEReferences) empty(gtx layout.Context, th *IDETheme) layout.Dimensions {
	return layout.Inset{Left: unit.Dp(10), Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		label := material.Label(th.Theme, th.Theme.TextSize, "No references")
		label.Font = th.MonoFont()
		label.Color = th.Muted
		label.MaxLines = 1
		return label.Layout(gtx)
	})
}

// row draws one reference as a clickable line: the muted "path:line:col"
// label, then the (single-line, truncated) preview of the target line.
func (r *IDEReferences) row(gtx layout.Context, th *IDETheme, wb *Workbench, index int) layout.Dimensions {
	loc := r.locations[index]
	return r.rows[index].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: unit.Dp(10), Right: unit.Dp(10), Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						label := material.Label(th.Theme, th.Theme.TextSize, r.displays[index])
						label.Font = th.MonoFont()
						label.Color = th.Muted
						label.MaxLines = 1
						return label.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						label := material.Label(th.Theme, th.Theme.TextSize, r.preview(wb, loc))
						label.Font = th.MonoFont()
						label.Color = th.Foreground
						label.MaxLines = 1
						return label.Layout(gtx)
					}),
				)
			})
	})
}

// preview returns the trimmed text of the reference's target line, or "" when
// it cannot be read. The active buffer is read live (its text may differ from
// disk); any other file is read from disk lazily, at most once per panel
// population, and skipped entirely when missing or larger than
// referencePreviewMaxFileBytes.
func (r *IDEReferences) preview(wb *Workbench, loc lsp.EditorLocation) string {
	line := loc.Range.Start.Line
	if tab, ok := wb.ActiveTab(); ok && pathsEqual(tab.Path, loc.Path) {
		if buf, err := wb.ActiveBuffer(); err == nil && buf != nil {
			if text, ok := buf.LineText(line); ok {
				return strings.TrimSpace(text)
			}
		}
		return ""
	}
	if r.previews == nil {
		r.previews = map[string][]string{}
	}
	lines, cached := r.previews[loc.Path]
	if !cached {
		lines = readPreviewLines(loc.Path)
		r.previews[loc.Path] = lines
	}
	if line < 0 || line >= len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[line])
}

// ensure resizes the per-row clickable slice to n, preserving existing
// entries so interaction state survives across frames when the count is
// unchanged.
func (r *IDEReferences) ensure(n int) {
	if len(r.rows) == n {
		return
	}
	rows := make([]*widget.Clickable, n)
	for i := 0; i < n; i++ {
		if i < len(r.rows) {
			rows[i] = r.rows[i]
			continue
		}
		rows[i] = &widget.Clickable{}
	}
	r.rows = rows
}

// referenceDisplayPath shortens path relative to the workbench root for
// display, falling back to the absolute path when there is no root or the
// path lies outside it.
func referenceDisplayPath(root, path string) string {
	if root == "" {
		return path
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	return rel
}

// readPreviewLines reads path once and splits it into lines for previews. It
// returns nil (no preview) when the file is missing, is a directory, or
// exceeds referencePreviewMaxFileBytes. Trailing carriage returns are left to
// the caller's TrimSpace.
func readPreviewLines(path string) []string {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > referencePreviewMaxFileBytes {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Split(string(data), "\n")
}
