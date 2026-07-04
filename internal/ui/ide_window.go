//go:build gio

package ui

import (
	"context"
	"errors"
	"image"

	gioapp "gioui.org/app"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
)

// Default IDE window geometry and title used when options leave them unset.
const (
	defaultIDEWidthDP  = 1100
	defaultIDEHeightDP = 740
	defaultIDETitle    = "Kleiber"
	ideTreeWidthDP     = 260
)

// ErrNilWorkbench is returned when RunIDEWindow is called without a
// workbench.
var ErrNilWorkbench = errors.New("ui: nil workbench")

// ideWindow holds the widgets and theme built once for the window's lifetime.
type ideWindow struct {
	theme     *IDETheme
	tree      *IDETree
	tabs      *IDETabs
	editor    *IDEEditor
	statusBar *IDEStatusBar
	problems  *IDEProblems

	// showProblems tracks whether the problems panel is expanded. It is
	// toggled by Ctrl+M and by clicking the status bar's problem count.
	showProblems bool
}

// newIDEWindow constructs the widget set for the IDE window.
func newIDEWindow() *ideWindow {
	return &ideWindow{
		theme:     NewIDETheme(),
		tree:      NewIDETree(),
		tabs:      NewIDETabs(),
		editor:    NewIDEEditor(),
		statusBar: NewIDEStatusBar(),
		problems:  NewIDEProblems(),
	}
}

// RunIDEWindow runs the read-only IDE window event loop over wb. It mirrors
// RunGioWindow's platform contract: the caller is responsible for invoking
// gioapp.Main from the program's main goroutine. It returns when the window
// is destroyed, propagating any non-cancellation error.
func RunIDEWindow(ctx context.Context, wb *Workbench, opts IDEWindowOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if wb == nil {
		return ErrNilWorkbench
	}

	title := opts.Title
	if title == "" {
		title = defaultIDETitle
	}
	width := opts.WidthDP
	if width <= 0 {
		width = defaultIDEWidthDP
	}
	height := opts.HeightDP
	if height <= 0 {
		height = defaultIDEHeightDP
	}

	w := new(gioapp.Window)
	w.Option(
		gioapp.Title(title),
		gioapp.Size(unit.Dp(width), unit.Dp(height)),
	)

	view := newIDEWindow()
	var ops op.Ops

	for {
		event := w.Event()
		switch event := event.(type) {
		case gioapp.DestroyEvent:
			if event.Err != nil && !errors.Is(event.Err, context.Canceled) {
				return event.Err
			}
			return nil
		case gioapp.FrameEvent:
			gtx := gioapp.NewContext(&ops, event)
			view.handleIDEKeys(gtx, wb, w)
			if view.layout(gtx, wb) {
				w.Invalidate()
			}
			event.Frame(&ops)
		}
	}
}

// handleIDEKeys processes the window-level key bindings that are independent
// of editor focus: Ctrl+W closes the active tab, Ctrl+F opens the find bar
// (focusing its query field), and Ctrl+M toggles the problems panel. Per-editor
// movement and text editing are handled inside IDEEditor while it holds focus.
func (v *ideWindow) handleIDEKeys(gtx layout.Context, wb *Workbench, w *gioapp.Window) {
	for {
		ev, ok := gtx.Event(
			key.Filter{Name: key.Name("W"), Required: key.ModShortcut},
			key.Filter{Name: key.Name("F"), Required: key.ModShortcut},
			key.Filter{Name: key.Name("M"), Required: key.ModShortcut},
		)
		if !ok {
			return
		}
		ke, ok := ev.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		switch ke.Name {
		case key.Name("W"):
			if idx := wb.ActiveIndex(); idx >= 0 {
				_ = wb.CloseTab(idx)
				w.Invalidate()
			}
		case key.Name("F"):
			if _, hasTab := wb.ActiveTab(); hasTab {
				v.editor.OpenFind(gtx)
				w.Invalidate()
			}
		case key.Name("M"):
			v.showProblems = !v.showProblems
			w.Invalidate()
		}
	}
}

// layout draws the whole window: file tree, a divider, then the tab strip
// over the editor. It reports whether any interaction changed state so the
// caller can request a redraw.
func (v *ideWindow) layout(gtx layout.Context, wb *Workbench) bool {
	paint.FillShape(gtx.Ops, v.theme.Background, clip.Rect{Max: gtx.Constraints.Max}.Op())
	changed := false

	layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			treeWidth := gtx.Metric.Dp(unit.Dp(ideTreeWidthDP))
			gtx.Constraints.Min.X = treeWidth
			gtx.Constraints.Max.X = treeWidth
			dims, ch := v.tree.Layout(gtx, v.theme, wb)
			if ch {
				changed = true
			}
			return dims
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.divider(gtx)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			children := []layout.FlexChild{
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					dims, ch := v.tabs.Layout(gtx, v.theme, wb)
					if ch {
						changed = true
					}
					return dims
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					dims, ch := v.editor.Layout(gtx, v.theme, wb)
					if ch {
						changed = true
					}
					return dims
				}),
			}
			if v.showProblems {
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					dims, jump := v.problems.Layout(gtx, v.theme, wb)
					if jump != nil && v.navigateTo(wb, jump) {
						changed = true
					}
					return dims
				}))
			}
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				dims, toggled := v.statusBar.Layout(gtx, v.theme, wb)
				if toggled {
					v.showProblems = !v.showProblems
					changed = true
				}
				return dims
			}))
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		}),
	)
	return changed
}

// navigateTo moves the caret to a problem's location and scrolls the editor so
// the line is near the top of the viewport. The problems panel lists only the
// active buffer's diagnostics, so the jump targets the active view; a mismatch
// (or missing view) is a no-op. It reports whether it changed state.
func (v *ideWindow) navigateTo(wb *Workbench, jump *problemJump) bool {
	tab, ok := wb.ActiveTab()
	if !ok || tab.BufferID != jump.BufferID {
		return false
	}
	view, err := wb.ActiveView()
	if err != nil || view == nil {
		return false
	}
	if err := view.MoveTo(jump.Pos); err != nil {
		return false
	}
	// Best-effort reveal: put the target line at the top of the viewport on
	// the next frame, matching the find bar's scroll behavior.
	v.editor.list.Position.First = jump.Pos.Line
	v.editor.list.Position.Offset = 0
	return true
}

// divider draws a 1px vertical separator sized to the available height.
func (v *ideWindow) divider(gtx layout.Context) layout.Dimensions {
	width := 1
	size := image.Point{X: width, Y: gtx.Constraints.Max.Y}
	paint.FillShape(gtx.Ops, v.theme.Divider, clip.Rect{Max: size}.Op())
	return layout.Dimensions{Size: size}
}
