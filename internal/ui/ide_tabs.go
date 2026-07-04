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

// IDETabs renders the horizontal strip of open editor tabs. The active tab is
// highlighted with an accent underline; each tab activates on click and
// exposes a small close control. Clickables are kept in slices sized to the
// tab count and rebuilt when the count changes.
type IDETabs struct {
	selects []*widget.Clickable
	closes  []*widget.Clickable
}

// NewIDETabs constructs an empty tab strip.
func NewIDETabs() *IDETabs {
	return &IDETabs{}
}

// Layout draws the tab strip for wb and dispatches activate/close clicks. It
// reports whether a click changed workbench state.
func (t *IDETabs) Layout(gtx layout.Context, th *IDETheme, wb *Workbench) (layout.Dimensions, bool) {
	tabs := wb.Tabs()
	t.ensure(len(tabs))
	active := wb.ActiveIndex()
	changed := false

	height := th.LineHeight(gtx) + gtx.Metric.Dp(unit.Dp(10))
	paint.FillShape(gtx.Ops, th.Panel, clip.Rect{Max: image.Point{X: gtx.Constraints.Max.X, Y: height}}.Op())

	if len(tabs) == 0 {
		return layout.Dimensions{Size: image.Point{X: gtx.Constraints.Max.X, Y: height}}, false
	}

	children := make([]layout.FlexChild, 0, len(tabs))
	for i := range tabs {
		i := i
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if t.tab(gtx, th, wb, tabs[i], i, i == active) {
				changed = true
			}
			return layout.Dimensions{Size: image.Point{}}
		}))
	}

	// Constrain the strip height so tabs lay out on one row.
	gtx.Constraints.Min.Y = height
	gtx.Constraints.Max.Y = height
	dims := layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
	dims.Size.X = gtx.Constraints.Max.X
	dims.Size.Y = height
	return dims, changed
}

// tab draws a single tab (label, dirty marker, close control) and handles its
// activate and close clicks. It returns true when a click changed state.
func (t *IDETabs) tab(gtx layout.Context, th *IDETheme, wb *Workbench, tab Tab, index int, active bool) bool {
	changed := false
	if t.selects[index].Clicked(gtx) {
		wb.Activate(index)
		changed = true
	}
	if t.closes[index].Clicked(gtx) {
		_ = wb.CloseTab(index)
		return true
	}

	name := tab.Name
	if wb.BufferDirty(tab.BufferID) {
		name += " *"
	}

	t.selects[index].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				bg := th.Panel
				if active {
					bg = th.ActiveTab
				}
				size := image.Point{X: gtx.Constraints.Min.X, Y: gtx.Constraints.Max.Y}
				paint.FillShape(gtx.Ops, bg, clip.Rect{Max: size}.Op())
				if active {
					barH := gtx.Metric.Dp(unit.Dp(2))
					stack := clip.Rect{
						Min: image.Point{X: 0, Y: size.Y - barH},
						Max: size,
					}.Push(gtx.Ops)
					paint.Fill(gtx.Ops, th.Accent)
					stack.Pop()
				}
				return layout.Dimensions{Size: size}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{
					Left:   unit.Dp(12),
					Right:  unit.Dp(6),
					Top:    unit.Dp(5),
					Bottom: unit.Dp(5),
				}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							label := material.Body2(th.Theme, name)
							label.Font = th.MonoFont()
							label.MaxLines = 1
							if active {
								label.Color = th.Foreground
							} else {
								label.Color = th.Muted
							}
							return label.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return t.closes[index].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								label := material.Body2(th.Theme, "x")
								label.Font = th.MonoFont()
								label.MaxLines = 1
								label.Color = th.Muted
								return label.Layout(gtx)
							})
						}),
					)
				})
			}),
		)
	})
	return changed
}

// ensure resizes the click-state slices to n, preserving existing entries so
// interaction state survives across frames when the count is unchanged.
func (t *IDETabs) ensure(n int) {
	if len(t.selects) == n {
		return
	}
	selects := make([]*widget.Clickable, n)
	closes := make([]*widget.Clickable, n)
	for i := 0; i < n; i++ {
		if i < len(t.selects) {
			selects[i] = t.selects[i]
			closes[i] = t.closes[i]
			continue
		}
		selects[i] = &widget.Clickable{}
		closes[i] = &widget.Clickable{}
	}
	t.selects = selects
	t.closes = closes
}
