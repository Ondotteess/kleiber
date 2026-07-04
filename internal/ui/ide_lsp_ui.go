//go:build gio

package ui

import (
	"context"
	"image"
	"image/color"
	"time"
	"unicode"
	"unicode/utf8"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"github.com/Ondotteess/kleiber/internal/editor"
	"github.com/Ondotteess/kleiber/internal/lsp"
)

// lspRequestTimeout bounds how long an async LSP request may run before its
// context is cancelled and the goroutine exits. It is well above gopls'
// typical latency yet short enough that a stuck server never leaks a
// goroutine for the process' lifetime.
const lspRequestTimeout = 5 * time.Second

// completionMaxVisibleRows caps how many completion rows are shown at once;
// longer lists scroll inside the popup.
const completionMaxVisibleRows = 12

// hoverMaxWidthDp bounds the hover tooltip's width so long documentation
// wraps instead of running off-screen.
const hoverMaxWidthDp = 480

// popupPadDp is the inner padding of an LSP popup box.
const popupPadDp = 6

// completionState is the mutex-guarded UI state of the completion popup. The
// launching goroutine seeds anchor/epoch; the async handler fills items under
// the lock only when its epoch still matches (see IDEEditor.lspMu). selected
// and the open flag are then mutated on the UI goroutine as the user
// navigates.
type completionState struct {
	open     bool
	items    []lsp.CompletionItem
	selected int
	anchor   editor.Position
	epoch    uint64
}

// hoverState is the mutex-guarded UI state of the hover tooltip. text is the
// plaintext hover contents; epoch drops stale async results the same way
// completionState does.
type hoverState struct {
	open   bool
	text   string
	anchor editor.Position
	epoch  uint64
}

// handleLSPKey gives the LSP popups first refusal on a pressed key. It returns
// (consumed, changed): consumed means the key was fully handled here and must
// not reach normal editing; changed means UI state changed and a redraw is
// warranted.
//
// Key routing:
//   - Ctrl+Space / F1 / F12 launch completion / hover / definition and are
//     always consumed.
//   - While the completion popup is open, Up/Down move the selection,
//     Enter/Tab apply it, and Esc closes it — all consumed.
//   - Escape closes the hover tooltip (consumed only when a popup was open).
//   - Any other key closes open popups but is left unconsumed, so typing or
//     caret movement still happens as usual and the now-stale popup vanishes.
func (e *IDEEditor) handleLSPKey(gtx layout.Context, wb *Workbench, ke key.Event) (consumed, changed bool) {
	shortcut := ke.Modifiers.Contain(key.ModShortcut)

	// Triggers first: they apply whether or not a popup is already open.
	switch {
	case shortcut && ke.Name == key.NameSpace:
		e.triggerCompletion(wb)
		return true, true
	case !shortcut && ke.Name == key.NameF1:
		e.triggerHover(wb)
		return true, true
	case !shortcut && ke.Name == key.NameF12:
		e.triggerDefinition(wb)
		return true, true
	}

	// While the completion popup is open, its navigation keys act on the
	// popup rather than the buffer.
	if e.completionOpen() {
		switch ke.Name {
		case key.NameUpArrow:
			e.moveCompletion(-1)
			return true, true
		case key.NameDownArrow:
			e.moveCompletion(+1)
			return true, true
		case key.NameReturn, key.NameEnter, key.NameTab:
			return true, e.applyCompletion(wb)
		case key.NameEscape:
			e.closeCompletion()
			return true, true
		default:
			// Any other key (typing, arrows left/right, etc.) dismisses the
			// popup but is handled normally so the character still types or
			// the caret still moves.
			e.closeCompletion()
			return false, true
		}
	}

	// With only the hover tooltip open, Escape closes it; any other key
	// (which will move the caret or type) also closes it but is not consumed.
	if e.hoverOpen() {
		if ke.Name == key.NameEscape {
			e.closeHover()
			return true, true
		}
		e.closeHover()
		return false, true
	}

	return false, false
}

// triggerCompletion launches an async completion request at the caret of the
// active view. It is a no-op when there is no LSP controller, no active view,
// or no active tab. The result is stored under the lock only if the captured
// epoch is still current when it arrives, then a redraw is scheduled.
func (e *IDEEditor) triggerCompletion(wb *Workbench) {
	c := wb.LSP()
	if c == nil {
		return
	}
	tab, ok := wb.ActiveTab()
	if !ok {
		return
	}
	view, err := wb.ActiveView()
	if err != nil || view == nil {
		return
	}
	pos := view.Selection().Cursor

	e.lspMu.Lock()
	e.completion.epoch++
	epoch := e.completion.epoch
	// Seed the anchor so a result that lands after a small caret move still
	// positions against where completion was requested.
	e.completion.anchor = pos
	e.lspMu.Unlock()

	id := tab.BufferID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), lspRequestTimeout)
		defer cancel()
		list, err := c.Complete(ctx, id, pos)
		if err != nil || list == nil || len(list.Items) == 0 {
			return
		}
		e.lspMu.Lock()
		if e.completion.epoch == epoch {
			e.completion.open = true
			e.completion.items = list.Items
			e.completion.selected = 0
			e.completion.anchor = pos
		}
		e.lspMu.Unlock()
		e.doInvalidate()
	}()
}

// triggerHover launches an async hover request at the caret of the active
// view, storing plaintext contents for the tooltip. Empty contents show
// nothing.
func (e *IDEEditor) triggerHover(wb *Workbench) {
	c := wb.LSP()
	if c == nil {
		return
	}
	tab, ok := wb.ActiveTab()
	if !ok {
		return
	}
	view, err := wb.ActiveView()
	if err != nil || view == nil {
		return
	}
	pos := view.Selection().Cursor

	e.lspMu.Lock()
	e.hover.epoch++
	epoch := e.hover.epoch
	e.hover.anchor = pos
	e.lspMu.Unlock()

	id := tab.BufferID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), lspRequestTimeout)
		defer cancel()
		hv, err := c.Hover(ctx, id, pos)
		if err != nil || hv == nil {
			return
		}
		// EditorHover.Contents is an lsp.MarkupContent; its Value carries the
		// plaintext (or markdown source) the tooltip renders as-is.
		text := hv.Contents.Value
		if text == "" {
			return
		}
		e.lspMu.Lock()
		if e.hover.epoch == epoch {
			e.hover.open = true
			e.hover.text = text
			e.hover.anchor = pos
		}
		e.lspMu.Unlock()
		e.doInvalidate()
	}()
}

// triggerDefinition launches an async go-to-definition request at the caret.
// On a non-empty result it opens the first location's file (activating its
// tab), moves that view's caret to the definition, and scrolls it into view —
// all on the request goroutine, since the workbench and engine are
// thread-safe. Cross-file jumps are supported because the location's path may
// differ from the current file.
func (e *IDEEditor) triggerDefinition(wb *Workbench) {
	c := wb.LSP()
	if c == nil {
		return
	}
	tab, ok := wb.ActiveTab()
	if !ok {
		return
	}
	view, err := wb.ActiveView()
	if err != nil || view == nil {
		return
	}
	pos := view.Selection().Cursor
	id := tab.BufferID

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), lspRequestTimeout)
		defer cancel()
		locs, err := c.Definition(ctx, id, pos)
		if err != nil || len(locs) == 0 {
			return
		}
		loc := locs[0]
		if _, err := wb.OpenFile(context.Background(), loc.Path); err != nil {
			return
		}
		target, err := wb.ActiveView()
		if err != nil || target == nil {
			return
		}
		if err := target.MoveTo(loc.Range.Start); err != nil {
			return
		}
		// Queue the definition line to be revealed at the top of the viewport
		// on the next frame. The scroll is applied on the UI goroutine (see
		// applyPendingReveal) rather than written to list.Position here, so the
		// widget.List is never mutated off the render goroutine.
		e.lspMu.Lock()
		e.revealLine = loc.Range.Start.Line
		e.lspMu.Unlock()
		e.doInvalidate()
	}()
}

// applyPendingReveal consumes a scroll target queued by an async
// go-to-definition and applies it to list. It runs on the UI goroutine at the
// start of Layout, keeping list.Position writes single-threaded.
func (e *IDEEditor) applyPendingReveal() {
	e.lspMu.Lock()
	line := e.revealLine
	e.revealLine = -1
	e.lspMu.Unlock()
	if line < 0 {
		return
	}
	e.list.Position.First = line
	e.list.Position.Offset = 0
}

// doInvalidate schedules a window redraw if an invalidate hook is set.
func (e *IDEEditor) doInvalidate() {
	if e.invalidate != nil {
		e.invalidate()
	}
}

// dismissLSPPopupsOnEdit closes any open completion or hover popup because the
// buffer is about to change (the user typed). It reports whether a popup was
// open, so the caller can note the state change. Epochs are bumped so an
// in-flight request cannot re-open a popup that the edit invalidated.
func (e *IDEEditor) dismissLSPPopupsOnEdit() bool {
	e.lspMu.Lock()
	defer e.lspMu.Unlock()
	changed := false
	if e.completion.open {
		e.completion.open = false
		e.completion.items = nil
		e.completion.selected = 0
		e.completion.epoch++
		changed = true
	}
	if e.hover.open {
		e.hover.open = false
		e.hover.text = ""
		e.hover.epoch++
		changed = true
	}
	return changed
}

// completionOpen reports whether the completion popup is currently open.
func (e *IDEEditor) completionOpen() bool {
	e.lspMu.Lock()
	defer e.lspMu.Unlock()
	return e.completion.open
}

// hoverOpen reports whether the hover tooltip is currently open.
func (e *IDEEditor) hoverOpen() bool {
	e.lspMu.Lock()
	defer e.lspMu.Unlock()
	return e.hover.open
}

// moveCompletion shifts the selected completion row by delta, clamped to the
// list bounds.
func (e *IDEEditor) moveCompletion(delta int) {
	e.lspMu.Lock()
	defer e.lspMu.Unlock()
	if !e.completion.open || len(e.completion.items) == 0 {
		return
	}
	sel := e.completion.selected + delta
	if sel < 0 {
		sel = 0
	}
	if sel >= len(e.completion.items) {
		sel = len(e.completion.items) - 1
	}
	e.completion.selected = sel
}

// closeCompletion hides the completion popup and drops its items. Bumping the
// epoch cancels any in-flight request so a late result cannot re-open it.
func (e *IDEEditor) closeCompletion() {
	e.lspMu.Lock()
	e.completion.open = false
	e.completion.items = nil
	e.completion.selected = 0
	e.completion.epoch++
	e.lspMu.Unlock()
}

// closeHover hides the hover tooltip. Bumping the epoch cancels any in-flight
// request so a late result cannot re-open it.
func (e *IDEEditor) closeHover() {
	e.lspMu.Lock()
	e.hover.open = false
	e.hover.text = ""
	e.hover.epoch++
	e.lspMu.Unlock()
}

// applyCompletion inserts the selected item at the caret, replacing the
// identifier prefix already typed on the cursor line, then closes the popup.
// It reports whether it changed buffer state.
//
// Insertion strategy (prefix replacement, deliberately NOT LSP TextEdit): LSP
// items may carry a textEdit whose range would have to be mapped from UTF-16
// back to editor byte columns and reconciled against edits made since the
// request. That is fragile for an async popup, so instead we scan back over
// the identifier characters immediately before the caret (Unicode letters,
// digits, underscore), select that prefix, and replace it with the item's
// InsertText (or Label when InsertText is empty). For ordinary member/name
// completions this produces the expected result without any range mapping.
func (e *IDEEditor) applyCompletion(wb *Workbench) bool {
	e.lspMu.Lock()
	if !e.completion.open || len(e.completion.items) == 0 {
		e.lspMu.Unlock()
		return false
	}
	item := e.completion.items[e.completion.selected]
	e.lspMu.Unlock()

	view, err := wb.ActiveView()
	if err != nil || view == nil {
		e.closeCompletion()
		return false
	}
	buf := view.Buffer()
	if buf == nil {
		e.closeCompletion()
		return false
	}

	insertion := item.InsertText
	if insertion == "" {
		insertion = item.Label
	}

	cursor := view.Selection().Cursor
	lineText, _ := buf.LineText(cursor.Line)
	prefixStart := identifierPrefixStart(lineText, cursor.Column)

	// Replace the typed prefix with the completion in one edit: select from
	// the prefix start to the caret, then insert (which overwrites the
	// selection).
	if prefixStart < cursor.Column {
		startPos := editor.Position{Line: cursor.Line, Column: prefixStart}
		if err := view.SetSelection(editor.NewSelection(startPos, cursor)); err != nil {
			e.closeCompletion()
			return false
		}
	}
	if _, err := view.InsertText(insertion); err != nil {
		e.closeCompletion()
		return false
	}
	e.closeCompletion()
	e.doInvalidate()
	return true
}

// identifierPrefixStart returns the byte column at which the identifier ending
// at byteCol on line begins. It scans backward over runes that are Unicode
// letters, digits, or underscore, stopping at the first rune that is not part
// of an identifier. When byteCol sits at a non-identifier boundary it returns
// byteCol unchanged (empty prefix).
func identifierPrefixStart(line string, byteCol int) int {
	if byteCol > len(line) {
		byteCol = len(line)
	}
	start := byteCol
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(line[:start])
		if size == 0 || !isIdentifierRune(r) {
			break
		}
		start -= size
	}
	return start
}

// isIdentifierRune reports whether r may appear inside an identifier prefix
// for completion purposes: a Unicode letter, a decimal digit, or underscore.
func isIdentifierRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// layoutLSPPopups draws the completion popup and hover tooltip over the editor
// text. Both are anchored near the caret, one line below it, and clamped to
// stay within the editor body. It reads the async state under the lock and
// copies what it needs before drawing.
func (e *IDEEditor) layoutLSPPopups(gtx layout.Context, th *IDETheme, buf *editor.Buffer, cursorLine, cursorCol, cell, lineHeight, gutterWidth, tabWidth int) {
	if buf == nil {
		return
	}

	e.lspMu.Lock()
	compOpen := e.completion.open
	compItems := e.completion.items
	compSelected := e.completion.selected
	hovOpen := e.hover.open
	hovText := e.hover.text
	e.lspMu.Unlock()

	if !compOpen && !hovOpen {
		return
	}

	lineText, _ := buf.LineText(cursorLine)
	visCol := VisualColumn(lineText, cursorCol, tabWidth)
	anchorX := gutterWidth + visCol*cell
	// One line below the caret. list.Position.First is the first visible line.
	anchorY := (cursorLine - e.list.Position.First + 1) * lineHeight

	if compOpen && len(compItems) > 0 {
		e.drawCompletionPopup(gtx, th, compItems, compSelected, anchorX, anchorY, cell, lineHeight)
	} else if hovOpen {
		e.drawHoverPopup(gtx, th, hovText, anchorX, anchorY)
	}
}

// drawCompletionPopup renders the bordered completion list at (anchorX,
// anchorY), clamped on-screen. Rows show the label (monospace) with the detail
// muted on the right; the selected row is filled with PopupSelected. Lists
// longer than completionMaxVisibleRows scroll.
func (e *IDEEditor) drawCompletionPopup(gtx layout.Context, th *IDETheme, items []lsp.CompletionItem, selected, anchorX, anchorY, cell, lineHeight int) {
	// Width: sized to the widest "label  detail" line, within sane bounds.
	labelChars := 0
	for _, it := range items {
		n := len(it.Label) + len(it.Detail)
		if it.Detail != "" {
			n += 2 // gap between label and detail
		}
		if n > labelChars {
			labelChars = n
		}
	}
	pad := gtx.Metric.Dp(unit.Dp(popupPadDp))
	minW := gtx.Metric.Dp(unit.Dp(120))
	maxW := gtx.Metric.Dp(unit.Dp(hoverMaxWidthDp))
	width := labelChars*cell + 2*pad
	if width < minW {
		width = minW
	}
	if width > maxW {
		width = maxW
	}

	rows := len(items)
	if rows > completionMaxVisibleRows {
		rows = completionMaxVisibleRows
	}
	height := rows*lineHeight + 2*pad

	x, y := clampPopup(gtx, anchorX, anchorY, width, height, lineHeight)
	e.fillPopupBox(gtx, th, x, y, width, height)

	// Row list, inset by the border padding.
	stack := op.Offset(image.Pt(x+pad, y+pad)).Push(gtx.Ops)
	inner := gtx
	inner.Constraints.Min = image.Point{}
	inner.Constraints.Max = image.Point{X: width - 2*pad, Y: height - 2*pad}
	material.List(th.Theme, &e.completionList).Layout(inner, len(items), func(gtx layout.Context, index int) layout.Dimensions {
		return completionRow(gtx, th, items[index], index == selected, width-2*pad, lineHeight)
	})
	stack.Pop()
}

// completionRow draws one completion entry: an optional selection fill, the
// label at the left, and the detail muted at the right.
func completionRow(gtx layout.Context, th *IDETheme, item lsp.CompletionItem, selected bool, rowWidth, lineHeight int) layout.Dimensions {
	size := image.Point{X: rowWidth, Y: lineHeight}
	if selected {
		paint.FillShape(gtx.Ops, th.PopupSelected, clip.Rect{Max: size}.Op())
	}

	label := material.Label(th.Theme, th.Theme.TextSize, item.Label)
	label.Font = th.MonoFont()
	label.Color = th.PopupText
	label.MaxLines = 1
	lg := gtx
	lg.Constraints.Min = image.Point{}
	lg.Constraints.Max = size
	label.Layout(lg)

	if item.Detail != "" {
		det := material.Label(th.Theme, th.Theme.TextSize, item.Detail)
		det.Font = th.MonoFont()
		det.Color = th.Muted
		det.MaxLines = 1
		det.Alignment = text.End
		dg := gtx
		dg.Constraints.Min = image.Point{}
		dg.Constraints.Max = size
		det.Layout(dg)
	}
	return layout.Dimensions{Size: size}
}

// drawHoverPopup renders the read-only hover tooltip at (anchorX, anchorY),
// wrapping text to at most hoverMaxWidthDp and clamped on-screen.
func (e *IDEEditor) drawHoverPopup(gtx layout.Context, th *IDETheme, hoverText string, anchorX, anchorY int) {
	pad := gtx.Metric.Dp(unit.Dp(popupPadDp))
	maxW := gtx.Metric.Dp(unit.Dp(hoverMaxWidthDp))

	// Measure the wrapped text off-screen to size the box.
	macro := op.Record(gtx.Ops)
	measure := gtx
	measure.Constraints.Min = image.Point{}
	measure.Constraints.Max = image.Point{X: maxW - 2*pad, Y: 1 << 20}
	lbl := material.Body2(th.Theme, hoverText)
	lbl.Font = th.MonoFont()
	lbl.Color = th.PopupText
	dims := lbl.Layout(measure)
	macro.Stop()

	width := dims.Size.X + 2*pad
	height := dims.Size.Y + 2*pad
	lineHeight := th.LineHeight(gtx)

	x, y := clampPopup(gtx, anchorX, anchorY, width, height, lineHeight)
	e.fillPopupBox(gtx, th, x, y, width, height)

	stack := op.Offset(image.Pt(x+pad, y+pad)).Push(gtx.Ops)
	inner := gtx
	inner.Constraints.Min = image.Point{}
	inner.Constraints.Max = image.Point{X: width - 2*pad, Y: height - 2*pad}
	txt := material.Body2(th.Theme, hoverText)
	txt.Font = th.MonoFont()
	txt.Color = th.PopupText
	txt.Layout(inner)
	stack.Pop()
}

// fillPopupBox paints the filled background and 1px border of a popup at the
// given rectangle.
func (e *IDEEditor) fillPopupBox(gtx layout.Context, th *IDETheme, x, y, width, height int) {
	stack := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
	paint.FillShape(gtx.Ops, th.PopupBg, clip.Rect{Max: image.Point{X: width, Y: height}}.Op())
	drawBorder(gtx, th.PopupBorder, width, height)
	stack.Pop()
}

// drawBorder strokes a 1px rectangle border of the given size at the current
// offset, using four thin filled rects (crisper than a stroked path at 1px).
func drawBorder(gtx layout.Context, col color.NRGBA, width, height int) {
	top := clip.Rect{Max: image.Point{X: width, Y: 1}}.Op()
	bottom := clip.Rect{Min: image.Point{X: 0, Y: height - 1}, Max: image.Point{X: width, Y: height}}.Op()
	left := clip.Rect{Max: image.Point{X: 1, Y: height}}.Op()
	right := clip.Rect{Min: image.Point{X: width - 1, Y: 0}, Max: image.Point{X: width, Y: height}}.Op()
	paint.FillShape(gtx.Ops, col, top)
	paint.FillShape(gtx.Ops, col, bottom)
	paint.FillShape(gtx.Ops, col, left)
	paint.FillShape(gtx.Ops, col, right)
}

// clampPopup shifts a popup of the given size so it stays within the editor
// body. When it would overflow the bottom, it flips to sit above the caret
// line instead; horizontal overflow is pushed left to fit.
func clampPopup(gtx layout.Context, x, y, width, height, lineHeight int) (int, int) {
	maxX := gtx.Constraints.Max.X
	maxY := gtx.Constraints.Max.Y

	if x+width > maxX {
		x = maxX - width
	}
	if x < 0 {
		x = 0
	}
	if y+height > maxY {
		// Flip above the caret line (y currently one line below the caret).
		above := y - lineHeight - height
		if above >= 0 {
			y = above
		} else {
			y = maxY - height
		}
	}
	if y < 0 {
		y = 0
	}
	return x, y
}
