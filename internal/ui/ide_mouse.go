//go:build gio

package ui

import (
	"image"
	"math"

	"gioui.org/gesture"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"

	"github.com/Ondotteess/kleiber/internal/editor"
)

// registerMouse marks area (the editor body) as the target for the editor's
// click and drag gestures. The gestures claim only press/drag/release-style
// pointer kinds, so mouse-wheel pointer.Scroll events keep flowing to the
// material.List that scrolls the text. It must be called while area is the
// active clip, before the list draws, so hits are bounded to the editor body.
func (e *IDEEditor) registerMouse(gtx layout.Context, area clip.Rect) {
	defer area.Push(gtx.Ops).Pop()
	e.click.Add(gtx.Ops)
	e.drag.Add(gtx.Ops)
}

// processMouse drains this frame's click and drag gesture events and applies
// them to the active view: press places the caret (Shift extends the
// selection), dragging extends it continuously, double-click selects the word
// under the pointer and triple-click selects the line. It returns true when
// any event mutated selection state; it also schedules a redraw through the
// invalidate hook so the new caret is drawn in the very next frame.
//
// It must run before the list draws so a click is reflected in the same
// frame's paint. Auto-scrolling while dragging past the viewport edge is
// intentionally not implemented.
func (e *IDEEditor) processMouse(gtx layout.Context, th *IDETheme, wb *Workbench) bool {
	changed := false
	for {
		evt, ok := e.click.Update(gtx.Source)
		if !ok {
			break
		}
		if e.applyClick(gtx, th, wb, evt) {
			changed = true
		}
	}
	for {
		evt, ok := e.drag.Update(gtx.Metric, gtx.Source, gesture.Both)
		if !ok {
			break
		}
		if e.applyDrag(gtx, th, wb, evt) {
			changed = true
		}
	}
	if changed && e.invalidate != nil {
		e.invalidate()
	}
	return changed
}

// applyClick handles one click-gesture event. A mouse press (or a completed
// non-mouse tap) focuses the editor and places or extends the selection
// according to the click count and Shift modifier. It returns true when the
// selection changed.
func (e *IDEEditor) applyClick(gtx layout.Context, th *IDETheme, wb *Workbench, evt gesture.ClickEvent) bool {
	switch {
	case evt.Kind == gesture.KindPress && evt.Source == pointer.Mouse,
		evt.Kind == gesture.KindClick && evt.Source != pointer.Mouse:
	case evt.Kind == gesture.KindCancel:
		e.mouseDragging = false
		return false
	default:
		return false
	}

	// Any press in the editor body focuses it so subsequent keystrokes route
	// here, even when no tab is open.
	gtx.Execute(key.FocusCmd{Tag: e.inputTag()})

	view, err := wb.ActiveView()
	if err != nil || view == nil {
		return false
	}
	pos, lineText, ok := e.mousePosition(gtx, th, wb, evt.Position)
	if !ok {
		return false
	}

	switch {
	case evt.NumClicks >= 3:
		// Triple-click selects the whole line, newline excluded.
		return view.SetSelection(editor.NewSelection(
			editor.Position{Line: pos.Line, Column: 0},
			editor.Position{Line: pos.Line, Column: len(lineText)},
		)) == nil
	case evt.NumClicks == 2:
		start, end := WordBoundsAt(lineText, pos.Column)
		return view.SetSelection(editor.NewSelection(
			editor.Position{Line: pos.Line, Column: start},
			editor.Position{Line: pos.Line, Column: end},
		)) == nil
	case evt.Modifiers.Contain(key.ModShift):
		// Shift-click extends the existing selection to the click point.
		e.mouseDragging = true
		return view.MoveCursorTo(pos) == nil
	default:
		e.mouseDragging = true
		return view.MoveTo(pos) == nil
	}
}

// applyDrag handles one drag-gesture pointer event. While a press started in
// the editor body is held, each mouse drag (and the final release) moves the
// cursor end of the selection to the pointer, so the selection follows the
// drag. It returns true when the selection changed.
func (e *IDEEditor) applyDrag(gtx layout.Context, th *IDETheme, wb *Workbench, evt pointer.Event) bool {
	release := false
	switch {
	case evt.Kind == pointer.Cancel:
		e.mouseDragging = false
		return false
	case evt.Kind == pointer.Release && evt.Source == pointer.Mouse:
		release = true
	case evt.Kind == pointer.Drag && evt.Source == pointer.Mouse:
	default:
		return false
	}
	if !e.mouseDragging {
		return false
	}
	if release {
		e.mouseDragging = false
	}

	view, err := wb.ActiveView()
	if err != nil || view == nil {
		return false
	}
	pt := image.Point{
		X: int(math.Round(float64(evt.Position.X))),
		Y: int(math.Round(float64(evt.Position.Y))),
	}
	pos, _, ok := e.mousePosition(gtx, th, wb, pt)
	if !ok {
		return false
	}
	return view.MoveCursorTo(pos) == nil
}

// mousePosition maps a pointer position in editor-body coordinates to a
// buffer position, using the same per-frame metrics the renderer uses: the
// list scroll state locates the line and the monospace cell width locates the
// visual column, which ByteColForVisual snaps to a byte column on a rune
// boundary. Clicks in the gutter map to column 0. It also returns the text of
// the hit line. ok is false when no buffer is open.
func (e *IDEEditor) mousePosition(gtx layout.Context, th *IDETheme, wb *Workbench, pt image.Point) (pos editor.Position, lineText string, ok bool) {
	buf, err := wb.ActiveBuffer()
	if err != nil || buf == nil {
		return editor.Position{}, "", false
	}

	lineHeight := th.LineHeight(gtx)
	cell := th.CellWidth(gtx)
	gutter := e.gutterWidth(gtx, th, buf.Lines())
	tabWidth := editorTabWidth(wb)

	// A grabbed drag can report positions above the viewport, so floor the
	// division (Go's integer division truncates toward zero) before clamping.
	y := pt.Y + e.list.Position.Offset
	line := e.list.Position.First + int(math.Floor(float64(y)/float64(lineHeight)))
	if line < 0 {
		line = 0
	}
	if last := buf.Lines() - 1; line > last {
		line = last
	}

	x := pt.X - gutter
	if x < 0 {
		x = 0
	}
	visual := int(math.Round(float64(x) / float64(cell)))

	lineText, _ = buf.LineText(line)
	col := ByteColForVisual(lineText, visual, tabWidth)
	return editor.Position{Line: line, Column: col}, lineText, true
}
