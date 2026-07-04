//go:build gio

package ui

import (
	"image"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/Ondotteess/kleiber/internal/editor"
)

// findBarHeightDp is the fixed height of the find overlay bar in
// device-independent pixels.
const findBarHeightDp = 30

// FindState holds the incremental-search UI state for one editor: whether the
// bar is open, the query field, and the match set cached against the buffer
// snapshot it was computed for. It imports no window state so the editor owns
// a single value.
type FindState struct {
	open  bool
	query widget.Editor

	// focusPending requests keyboard focus for the query field on the next
	// layout, used when the bar is opened via Ctrl+F.
	focusPending bool

	// Cached matches and the buffer identity/sequence they were computed
	// against, so FindAll runs only when the query or buffer changes.
	matches    []editor.Range
	lastQuery  string
	lastSeq    int
	lastLength int
	haveCache  bool
}

// NewFindState constructs a closed find state with a single-line, submit-on-
// enter query editor.
func NewFindState() FindState {
	return FindState{
		query: widget.Editor{SingleLine: true, Submit: true},
	}
}

// Open reports whether the find bar is currently visible.
func (f *FindState) Open() bool { return f.open }

// OpenFind opens the find bar and requests focus for its query field. It is a
// method on IDEEditor so callers reach it through the editor that owns the
// state.
func (e *IDEEditor) OpenFind(gtx layout.Context) {
	e.find.open = true
	e.find.focusPending = true
	e.find.query.SetCaret(0, e.find.query.Len())
	gtx.Execute(key.FocusCmd{Tag: &e.find.query})
}

// MatchesFor returns the cached matches for buf when the find bar is open,
// refreshing the cache if the query or buffer changed since the last
// computation. It returns nil when the bar is closed or the query is empty, so
// no highlights are drawn.
func (f *FindState) MatchesFor(buf *editor.Buffer) []editor.Range {
	if !f.open || buf == nil {
		return nil
	}
	f.refresh(buf)
	return f.matches
}

// refresh recomputes matches when the query text or buffer contents have
// changed since the cache was last built.
func (f *FindState) refresh(buf *editor.Buffer) {
	query := f.query.Text()
	seq := buf.Seq()
	length := buf.Length()
	if f.haveCache && f.lastQuery == query && f.lastSeq == seq && f.lastLength == length {
		return
	}
	if query == "" {
		f.matches = nil
	} else {
		f.matches = buf.FindAll(query, false)
	}
	f.lastQuery = query
	f.lastSeq = seq
	f.lastLength = length
	f.haveCache = true
}

// Layout draws the find bar over the top of the editor body and processes its
// input: query edits (which refresh matches), Enter / Shift+Enter to jump
// between matches, Ctrl+G for the next match, and Esc to close the bar and
// return focus to the code editor. It returns true when it changed editor,
// selection, or find state.
func (f *FindState) Layout(gtx layout.Context, th *IDETheme, wb *Workbench, host *IDEEditor, view *editor.View, buf *editor.Buffer) bool {
	changed := f.handleKeys(gtx, host, view, buf)

	// Drain query-editor events; a text change refreshes the match cache and
	// a SubmitEvent (Enter) jumps forward.
	for {
		ev, ok := f.query.Update(gtx)
		if !ok {
			break
		}
		switch ev.(type) {
		case widget.ChangeEvent:
			changed = true
		case widget.SubmitEvent:
			if f.jump(host, view, buf, false) {
				changed = true
			}
		}
	}

	if f.focusPending {
		f.focusPending = false
		gtx.Execute(key.FocusCmd{Tag: &f.query})
	}

	barHeight := gtx.Metric.Dp(unit.Dp(findBarHeightDp))
	width := gtx.Constraints.Max.X
	paint.FillShape(gtx.Ops, th.Panel, clip.Rect{Max: image.Point{X: width, Y: barHeight}}.Op())
	// Bottom divider under the bar.
	paint.FillShape(gtx.Ops, th.Divider, clip.Rect{
		Min: image.Point{X: 0, Y: barHeight - 1},
		Max: image.Point{X: width, Y: barHeight},
	}.Op())

	gtx.Constraints.Min = image.Point{}
	gtx.Constraints.Max.Y = barHeight
	layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(5), Bottom: unit.Dp(5)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					ed := material.Editor(th.Theme, &f.query, "Find")
					ed.Font = th.MonoFont()
					ed.Color = th.Foreground
					ed.HintColor = th.Muted
					return ed.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					label := material.Body2(th.Theme, f.countText(buf))
					label.Font = th.MonoFont()
					label.MaxLines = 1
					label.Color = th.Muted
					return label.Layout(gtx)
				}),
			)
		})
	return changed
}

// handleKeys processes the find bar's dedicated shortcuts. They filter on the
// query field's focus tag so they never fire while the code editor is focused,
// and typing in the field never leaks into the buffer.
func (f *FindState) handleKeys(gtx layout.Context, host *IDEEditor, view *editor.View, buf *editor.Buffer) bool {
	changed := false
	tag := &f.query
	for {
		ev, ok := gtx.Event(
			key.Filter{Focus: tag, Name: key.NameEscape},
			key.Filter{Focus: tag, Name: key.NameReturn, Required: key.ModShift},
			key.Filter{Focus: tag, Name: key.NameEnter, Required: key.ModShift},
			key.Filter{Focus: tag, Name: "G", Required: key.ModShortcut},
		)
		if !ok {
			break
		}
		ke, ok := ev.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		switch {
		case ke.Name == key.NameEscape:
			f.open = false
			gtx.Execute(key.FocusCmd{Tag: host.inputTag()})
			changed = true
		case ke.Name == "G":
			if f.jump(host, view, buf, false) {
				changed = true
			}
		default: // Shift+Enter: search backward.
			if f.jump(host, view, buf, true) {
				changed = true
			}
		}
	}
	return changed
}

// jump selects the next (or previous, when backward) match relative to the
// current cursor and scrolls the editor list so the match line is visible. It
// reports whether a match was selected. The scroll is applied to host.list for
// the next frame; the caller returns true so that frame is scheduled.
func (f *FindState) jump(host *IDEEditor, view *editor.View, buf *editor.Buffer, backward bool) bool {
	if view == nil || buf == nil {
		return false
	}
	f.refresh(buf)
	if len(f.matches) == 0 {
		return false
	}
	from := view.Selection().Cursor
	match, ok := editor.NextMatch(f.matches, from, backward)
	if !ok {
		return false
	}
	if err := view.SetSelection(editor.NewSelection(match.Start, match.End)); err != nil {
		return false
	}
	// Best-effort reveal: put the match's first line at the top of the
	// viewport on the next frame.
	host.list.Position.First = match.Start.Line
	host.list.Position.Offset = 0
	return true
}

// countText renders the match-count summary shown at the right of the bar.
func (f *FindState) countText(buf *editor.Buffer) string {
	if f.query.Len() == 0 {
		return ""
	}
	f.refresh(buf)
	n := len(f.matches)
	if n == 0 {
		return "No results"
	}
	if n == 1 {
		return "1 match"
	}
	return itoa(n) + " matches"
}
