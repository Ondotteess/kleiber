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
)

// statusBarHeightDp is the fixed height of the status bar in
// device-independent pixels.
const statusBarHeightDp = 22

// IDEStatusBar is the bottom bar summarizing editor and language-server state:
// the active file path on the left, then the caret position, a clickable
// problem count, and the language-server state on the right. It owns only the
// problem-count clickable; all displayed data is read synchronously from the
// workbench each frame.
type IDEStatusBar struct {
	problems widget.Clickable
}

// NewIDEStatusBar constructs an empty status bar.
func NewIDEStatusBar() *IDEStatusBar {
	return &IDEStatusBar{}
}

// Layout draws the status bar for wb and returns whether the problem-count
// segment was clicked this frame, so the caller can toggle the problems panel.
func (s *IDEStatusBar) Layout(gtx layout.Context, th *IDETheme, wb *Workbench) (layout.Dimensions, bool) {
	height := gtx.Metric.Dp(unit.Dp(statusBarHeightDp))
	width := gtx.Constraints.Max.X
	paint.FillShape(gtx.Ops, th.StatusBar, clip.Rect{Max: image.Point{X: width, Y: height}}.Op())

	toggled := s.problems.Clicked(gtx)

	gtx.Constraints.Min = image.Point{X: width, Y: height}
	gtx.Constraints.Max.Y = height
	layout.Inset{Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return s.label(gtx, th, s.fileText(wb))
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{Size: image.Point{X: gtx.Constraints.Min.X, Y: 0}}
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return s.label(gtx, th, s.caretText(wb))
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return s.problems.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return s.label(gtx, th, s.problemsText(wb))
				})
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return s.label(gtx, th, s.lspText(wb))
			}),
		)
	})

	return layout.Dimensions{Size: image.Point{X: width, Y: height}}, toggled
}

// label renders one status-bar segment as a single-line muted monospace label.
func (s *IDEStatusBar) label(gtx layout.Context, th *IDETheme, text string) layout.Dimensions {
	label := material.Label(th.Theme, th.Theme.TextSize, text)
	label.Font = th.MonoFont()
	label.Color = th.StatusText
	label.MaxLines = 1
	return label.Layout(gtx)
}

// fileText is the active file's path, or a placeholder when nothing is open.
func (s *IDEStatusBar) fileText(wb *Workbench) string {
	if tab, ok := wb.ActiveTab(); ok && tab.Path != "" {
		return tab.Path
	}
	return "no file"
}

// caretText is the 1-based caret position ("Ln L, Col C"), or empty when no
// view is active. The column is a byte offset, which is sufficient for the MVP.
func (s *IDEStatusBar) caretText(wb *Workbench) string {
	view, err := wb.ActiveView()
	if err != nil || view == nil {
		return ""
	}
	cur := view.Selection().Cursor
	return "Ln " + itoa(cur.Line+1) + ", Col " + itoa(cur.Column+1)
}

// problemsText summarizes the active buffer's diagnostic count. It returns
// "no problems" when there is nothing to report (or no controller/tab).
func (s *IDEStatusBar) problemsText(wb *Workbench) string {
	c := wb.LSP()
	tab, ok := wb.ActiveTab()
	if c == nil || !ok {
		return "no problems"
	}
	n := c.DiagnosticCount(tab.BufferID)
	switch n {
	case 0:
		return "no problems"
	case 1:
		return "1 problem"
	default:
		return itoa(n) + " problems"
	}
}

// lspText reports the language-server state, or "LSP: off" when no controller
// is attached.
func (s *IDEStatusBar) lspText(wb *Workbench) string {
	c := wb.LSP()
	if c == nil {
		return "LSP: off"
	}
	return "LSP: " + c.State().String()
}
