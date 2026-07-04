//go:build gio

package ui

import (
	"image"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/Ondotteess/kleiber/internal/editor"
)

// problemsPanelHeightDp is the fixed height of the problems panel when open.
const problemsPanelHeightDp = 160

// problemJump names a location a problems-panel row click wants to navigate to:
// the buffer and the diagnostic's start position.
type problemJump struct {
	// BufferID identifies the buffer the diagnostic belongs to.
	BufferID editor.BufferID
	// Pos is the diagnostic range's start, where the caret should land.
	Pos editor.Position
}

// IDEProblems is the collapsible panel listing the active buffer's diagnostics,
// one clickable row each. Clicking a row asks the window to jump to that
// diagnostic. It owns the scroll state and a per-row clickable slice rebuilt
// when the diagnostic count changes.
type IDEProblems struct {
	list widget.List
	rows []*widget.Clickable
}

// NewIDEProblems constructs an empty problems panel with a vertical scroll
// list.
func NewIDEProblems() *IDEProblems {
	return &IDEProblems{
		list: widget.List{List: layout.List{Axis: layout.Vertical}},
	}
}

// Layout draws the problems panel for the active buffer and returns the
// diagnostic a row click requested, or nil when nothing was clicked. The panel
// fills a fixed-height region: a header row over a scrolling diagnostic list.
func (p *IDEProblems) Layout(gtx layout.Context, th *IDETheme, wb *Workbench) (layout.Dimensions, *problemJump) {
	height := gtx.Metric.Dp(unit.Dp(problemsPanelHeightDp))
	width := gtx.Constraints.Max.X
	paint.FillShape(gtx.Ops, th.Panel, clip.Rect{Max: image.Point{X: width, Y: height}}.Op())
	// Top divider separating the panel from the editor above it.
	paint.FillShape(gtx.Ops, th.Divider, clip.Rect{Max: image.Point{X: width, Y: 1}}.Op())

	var diags []editor.Diagnostic
	var bufID editor.BufferID
	if tab, ok := wb.ActiveTab(); ok {
		bufID = tab.BufferID
		if c := wb.LSP(); c != nil {
			diags = c.Diagnostics(bufID)
		}
	}
	p.ensure(len(diags))

	var jump *problemJump

	gtx.Constraints.Min = image.Point{X: width, Y: height}
	gtx.Constraints.Max.Y = height
	layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return p.header(gtx, th, len(diags))
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if len(diags) == 0 {
				return p.empty(gtx, th)
			}
			return material.List(th.Theme, &p.list).Layout(gtx, len(diags), func(gtx layout.Context, index int) layout.Dimensions {
				if p.rows[index].Clicked(gtx) {
					jump = &problemJump{BufferID: bufID, Pos: diags[index].Range.Start}
				}
				return p.row(gtx, th, p.rows[index], diags[index])
			})
		}),
	)

	return layout.Dimensions{Size: image.Point{X: width, Y: height}}, jump
}

// header draws the "PROBLEMS" title with a trailing count.
func (p *IDEProblems) header(gtx layout.Context, th *IDETheme, count int) layout.Dimensions {
	return layout.Inset{Left: unit.Dp(10), Right: unit.Dp(10), Top: unit.Dp(5), Bottom: unit.Dp(5)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			text := "PROBLEMS"
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

// empty renders a muted placeholder when the active buffer has no diagnostics.
func (p *IDEProblems) empty(gtx layout.Context, th *IDETheme) layout.Dimensions {
	return layout.Inset{Left: unit.Dp(10), Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		label := material.Label(th.Theme, th.Theme.TextSize, "No problems")
		label.Font = th.MonoFont()
		label.Color = th.Muted
		label.MaxLines = 1
		return label.Layout(gtx)
	})
}

// row draws one diagnostic as a clickable line: a colored severity marker, the
// 1-based "line:col", then the (single-line, truncated) message.
func (p *IDEProblems) row(gtx layout.Context, th *IDETheme, btn *widget.Clickable, d editor.Diagnostic) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: unit.Dp(10), Right: unit.Dp(10), Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						marker := material.Label(th.Theme, th.Theme.TextSize, severityMarker(d.Severity))
						marker.Font = th.MonoFont()
						marker.Color = th.DiagnosticColor(d.Severity)
						marker.MaxLines = 1
						return marker.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						pos := itoa(d.Range.Start.Line+1) + ":" + itoa(d.Range.Start.Column+1)
						label := material.Label(th.Theme, th.Theme.TextSize, pos)
						label.Font = th.MonoFont()
						label.Color = th.Muted
						label.MaxLines = 1
						return label.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						label := material.Label(th.Theme, th.Theme.TextSize, d.Message)
						label.Font = th.MonoFont()
						label.Color = th.Foreground
						label.MaxLines = 1
						return label.Layout(gtx)
					}),
				)
			})
	})
}

// ensure resizes the per-row clickable slice to n, preserving existing entries
// so interaction state survives across frames when the count is unchanged.
func (p *IDEProblems) ensure(n int) {
	if len(p.rows) == n {
		return
	}
	rows := make([]*widget.Clickable, n)
	for i := 0; i < n; i++ {
		if i < len(p.rows) {
			rows[i] = p.rows[i]
			continue
		}
		rows[i] = &widget.Clickable{}
	}
	p.rows = rows
}

// severityMarker returns the single-character ASCII marker for a severity:
// "E" for error, "W" for warning, "i" for information/hint/unknown.
func severityMarker(sev editor.DiagnosticSeverity) string {
	switch sev {
	case editor.DiagnosticSeverityError:
		return "E"
	case editor.DiagnosticSeverityWarning:
		return "W"
	default:
		return "i"
	}
}
