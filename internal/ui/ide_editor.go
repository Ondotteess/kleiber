//go:build gio

package ui

import (
	"image"
	"strings"
	"sync"

	"gioui.org/gesture"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/Ondotteess/kleiber/internal/editor"
	"github.com/Ondotteess/kleiber/internal/syntax"
)

// caretWidthPx is the on-screen thickness of the caret bar in pixels.
const caretWidthPx = 2

// diagnosticUnderlinePx is the on-screen thickness of a diagnostic underline
// in pixels, drawn at the bottom edge of the row.
const diagnosticUnderlinePx = 2

// IDEEditor renders the active tab's buffer and edits it in place: a
// line-number gutter, syntax-highlighted text, the current-line highlight,
// selection overlays, find-match highlights and a caret. It owns the vertical
// scroll state, the find-bar state, and a per-buffer highlight cache so
// highlighting is recomputed only when the buffer's sequence changes.
type IDEEditor struct {
	list widget.List
	find FindState

	// Mouse gesture state for the editor body. click detects presses and
	// multi-clicks, drag reports pointer motion while pressed, and
	// mouseDragging is true between a caret-placing press and its release so
	// only drags that started in the editor extend the selection. All three
	// are touched only on the UI goroutine during Layout.
	click         gesture.Click
	drag          gesture.Drag
	mouseDragging bool

	// Highlight cache. lastSpans holds one span slice per line for the
	// buffer identified by lastBufferID at sequence lastSeq.
	lastBufferID editor.BufferID
	lastSeq      int
	lastLang     syntax.Language
	lastSpans    [][]syntax.Span

	// invalidate schedules a window redraw from any goroutine. It is set
	// once via SetInvalidate after the window is created and is safe to call
	// concurrently (Gio's Window.Invalidate is goroutine-safe). Async LSP
	// result handlers call it after storing their results so the popup
	// appears on the next frame. It may be nil before SetInvalidate runs.
	invalidate func()

	// lspMu guards the async LSP UI state below, which background request
	// goroutines write and the render loop reads. It is held only briefly to
	// copy what a frame needs.
	lspMu      sync.Mutex
	completion completionState
	hover      hoverState
	references referencesState

	// refsPanelOpen mirrors the window's references-panel visibility so the
	// editor's key handling can route Escape and the Shift+F12 toggle
	// correctly (the panel itself is owned by ideWindow). The window updates
	// it every frame via setReferencesPanelOpen before the editor lays out.
	// UI goroutine only.
	refsPanelOpen bool

	// refsCloseRequested is set when an editor key (Escape, Shift+F12) asks
	// the window to close the references panel; the window consumes it with
	// takeReferencesClose at the start of its next layout. UI goroutine only.
	refsCloseRequested bool

	// revealLine, when >= 0, is a scroll target set by an async
	// go-to-definition handler and applied to list on the next Layout. It is
	// funneled through lspMu (rather than written directly to list.Position
	// off-goroutine) so the widget.List is only ever mutated on the UI
	// goroutine, avoiding a data race with the render loop.
	revealLine int

	// completionList is the scroll state for the completion popup's rows. It
	// is touched only on the UI goroutine (during Layout), so it needs no
	// lock.
	completionList widget.List

	// autoFocusView is the view the editor last requested keyboard focus
	// for. When the active view changes (a file is opened or a tab is
	// switched), Layout focuses the editor once so typing works
	// immediately without requiring a click. UI-goroutine only.
	autoFocusView editor.ViewID
}

// NewIDEEditor constructs an editor widget with a vertical scroll list.
func NewIDEEditor() *IDEEditor {
	return &IDEEditor{
		list:           widget.List{List: layout.List{Axis: layout.Vertical}},
		find:           NewFindState(),
		completionList: widget.List{List: layout.List{Axis: layout.Vertical}},
		revealLine:     -1,
	}
}

// SetInvalidate stores the window's redraw hook so async LSP result handlers
// can schedule a frame after they store results. It is called once, after the
// window is created; fn must be safe to call from any goroutine (Gio's
// Window.Invalidate is).
func (e *IDEEditor) SetInvalidate(fn func()) {
	e.invalidate = fn
}

// Layout draws the active tab of wb and processes editor input for the frame.
// When no tab is open it centers a muted placeholder. The gutter scrolls with
// the text because each list item draws its own gutter cell beside the line.
// It returns true when input mutated buffer or selection state (or the find
// bar changed) so the caller can invalidate the window.
func (e *IDEEditor) Layout(gtx layout.Context, th *IDETheme, wb *Workbench) (layout.Dimensions, bool) {
	paint.FillShape(gtx.Ops, th.Background, clip.Rect{Max: gtx.Constraints.Max}.Op())

	// Apply any scroll target queued by an async go-to-definition handler.
	// Mutating list here (on the UI goroutine) keeps the widget.List free of
	// cross-goroutine writes.
	e.applyPendingReveal()

	// Route keys (when focused) and mouse gestures to the editor. Both areas
	// cover the whole editor body and are registered before the list draws;
	// events queued since the last frame are applied here, before painting,
	// so a click's caret move is visible in this same frame.
	e.registerInput(gtx, clip.Rect{Max: gtx.Constraints.Max})
	e.registerMouse(gtx, clip.Rect{Max: gtx.Constraints.Max})
	changed := e.handleInput(gtx, wb)
	if e.processMouse(gtx, th, wb) {
		changed = true
	}

	tab, ok := wb.ActiveTab()
	if !ok {
		return e.layoutEmpty(gtx, th), changed
	}
	buf, err := wb.ActiveBuffer()
	if err != nil || buf == nil {
		return e.layoutEmpty(gtx, th), changed
	}

	// Focus the editor when the active view changes (file opened or tab
	// switched) so typing works immediately without a click. The find
	// bar keeps its own focus while open: skip refocusing then.
	if tab.ViewID != e.autoFocusView && !e.find.Open() {
		e.autoFocusView = tab.ViewID
		gtx.Execute(key.FocusCmd{Tag: e.inputTag()})
	}

	view, _ := wb.ActiveView()
	spans := e.spansFor(tab, buf)

	// Diagnostics for the active buffer, read synchronously once per frame.
	// The controller may be absent (no language server); tolerate nil.
	var diags []editor.Diagnostic
	if c := wb.LSP(); c != nil {
		diags = c.Diagnostics(tab.BufferID)
	}

	tabWidth := editorTabWidth(wb)
	cell := th.CellWidth(gtx)
	lineHeight := th.LineHeight(gtx)
	gutterWidth := e.gutterWidth(gtx, th, buf.Lines())

	var cursorLine, cursorCol int
	haveCaret := false
	var selRange editor.Range
	haveSel := false
	if view != nil {
		sel := view.Selection()
		cursorLine = sel.Cursor.Line
		cursorCol = sel.Cursor.Column
		haveCaret = true
		if !sel.IsEmpty() {
			selRange = sel.AsRange()
			haveSel = true
		}
	}

	rd := rowDraw{
		theme:       th,
		buf:         buf,
		spans:       spans,
		tabWidth:    tabWidth,
		cell:        cell,
		lineHeight:  lineHeight,
		gutterWidth: gutterWidth,
		cursorLine:  cursorLine,
		cursorCol:   cursorCol,
		haveCaret:   haveCaret,
		selRange:    selRange,
		haveSel:     haveSel,
		findMatches: e.find.MatchesFor(buf),
		diags:       diags,
	}

	dims := material.List(th.Theme, &e.list).Layout(gtx, buf.Lines(), func(gtx layout.Context, index int) layout.Dimensions {
		return rd.line(gtx, index)
	})

	// The find bar overlays the top of the editor body when open.
	if e.find.Open() {
		if e.find.Layout(gtx, th, wb, e, view, buf) {
			changed = true
		}
	}

	// LSP popups (completion list, hover tooltip) float over the text,
	// anchored near the caret. They are drawn last so they sit above every
	// other editor layer.
	e.layoutLSPPopups(gtx, th, buf, cursorLine, cursorCol, cell, lineHeight, gutterWidth, tabWidth)
	return dims, changed
}

// layoutEmpty centers a muted placeholder when nothing is open.
func (e *IDEEditor) layoutEmpty(gtx layout.Context, th *IDETheme) layout.Dimensions {
	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		label := material.Body1(th.Theme, "No file open")
		label.Font = th.MonoFont()
		label.Color = th.Muted
		return label.Layout(gtx)
	})
}

// spansFor returns cached per-line spans for the buffer, recomputing only
// when the buffer identity or sequence changed. Non-Go files get nil spans
// (plain rendering).
func (e *IDEEditor) spansFor(tab Tab, buf *editor.Buffer) [][]syntax.Span {
	lang := syntax.LanguageForPath(tab.Path)
	seq := buf.Seq()
	if e.lastSpans != nil && e.lastBufferID == tab.BufferID && e.lastSeq == seq && e.lastLang == lang {
		return e.lastSpans
	}
	var spans [][]syntax.Span
	if lang == syntax.LanguageGo {
		spans = syntax.HighlightGo(buf.Text())
	}
	e.lastBufferID = tab.BufferID
	e.lastSeq = seq
	e.lastLang = lang
	e.lastSpans = spans
	return spans
}

// gutterWidth sizes the line-number gutter to the digit count of the last
// line plus padding, measured in pixels via the monospace cell width.
func (e *IDEEditor) gutterWidth(gtx layout.Context, th *IDETheme, lines int) int {
	digits := len(itoa(lines))
	if digits < 2 {
		digits = 2
	}
	pad := gtx.Metric.Dp(unit.Dp(16))
	return digits*th.CellWidth(gtx) + pad
}

// rowDraw carries the immutable per-frame parameters needed to draw one line
// row, so the list callback stays a thin closure.
type rowDraw struct {
	theme       *IDETheme
	buf         *editor.Buffer
	spans       [][]syntax.Span
	tabWidth    int
	cell        int
	lineHeight  int
	gutterWidth int
	cursorLine  int
	cursorCol   int
	haveCaret   bool
	selRange    editor.Range
	haveSel     bool
	findMatches []editor.Range
	diags       []editor.Diagnostic
}

// line draws a single editor row: current-line highlight, gutter number,
// selection overlay, text segments and caret.
func (rd rowDraw) line(gtx layout.Context, index int) layout.Dimensions {
	width := gtx.Constraints.Max.X
	rowSize := image.Point{X: width, Y: rd.lineHeight}

	lineText, _ := rd.buf.LineText(index)

	// Current-line highlight spans the full row width, drawn first.
	if rd.haveCaret && index == rd.cursorLine {
		paint.FillShape(gtx.Ops, rd.theme.LineHighlight, clip.Rect{Max: rowSize}.Op())
	}

	// Gutter background and right-aligned line number.
	rd.drawGutter(gtx, index)

	// Find-match highlights for this line (behind selection and text).
	rd.drawFindMatches(gtx, index, lineText)

	// Selection overlay for this line (behind text).
	if rd.haveSel {
		rd.drawSelection(gtx, index, lineText)
	}

	// Text segments, tab-expanded, positioned by visual column.
	rd.drawSegments(gtx, index, lineText)

	// Caret on the cursor line.
	if rd.haveCaret && index == rd.cursorLine {
		rd.drawCaret(gtx, lineText)
	}

	// Diagnostic underlines sit on top of the text at the row's bottom edge.
	rd.drawDiagnostics(gtx, index, lineText)

	return layout.Dimensions{Size: rowSize}
}

// drawGutter fills the gutter background and draws the 1-based line number
// right-aligned within it.
func (rd rowDraw) drawGutter(gtx layout.Context, index int) {
	gutterSize := image.Point{X: rd.gutterWidth, Y: rd.lineHeight}
	paint.FillShape(gtx.Ops, rd.theme.Gutter, clip.Rect{Max: gutterSize}.Op())

	num := itoa(index + 1)
	numWidth := len(num) * rd.cell
	x := rd.gutterWidth - gtx.Metric.Dp(unit.Dp(8)) - numWidth
	if x < 0 {
		x = 0
	}
	stack := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
	label := material.Label(rd.theme.Theme, rd.theme.Theme.TextSize, num)
	label.Font = rd.theme.MonoFont()
	label.Color = rd.theme.GutterText
	label.MaxLines = 1
	gutterGtx := gtx
	gutterGtx.Constraints.Min = image.Point{}
	gutterGtx.Constraints.Max = image.Point{X: numWidth + rd.cell, Y: rd.lineHeight}
	label.Layout(gutterGtx)
	stack.Pop()
}

// drawSelection draws the translucent selection rectangle for the portion of
// selRange that intersects this line. Fully-covered interior lines extend to
// the row's right edge.
func (rd rowDraw) drawSelection(gtx layout.Context, index int, lineText string) {
	start := rd.selRange.Start
	end := rd.selRange.End
	if index < start.Line || index > end.Line {
		return
	}

	startCol := 0
	if index == start.Line {
		startCol = start.Column
	}
	endByte := len(lineText)
	extendEOL := index < end.Line // interior/first line spanning to EOL
	if index == end.Line {
		endByte = end.Column
	}

	startVis := VisualColumn(lineText, startCol, rd.tabWidth)
	endVis := VisualColumn(lineText, endByte, rd.tabWidth)

	x0 := rd.gutterWidth + startVis*rd.cell
	x1 := rd.gutterWidth + endVis*rd.cell
	if extendEOL {
		// Include a trailing cell so the selection reads as "through the
		// newline" on wrapped/interior lines.
		x1 += rd.cell
	}
	if x1 <= x0 {
		if !extendEOL {
			return
		}
		x1 = x0 + rd.cell
	}

	stack := op.Offset(image.Pt(x0, 0)).Push(gtx.Ops)
	paint.FillShape(gtx.Ops, rd.theme.Selection, clip.Rect{Max: image.Point{X: x1 - x0, Y: rd.lineHeight}}.Op())
	stack.Pop()
}

// drawFindMatches draws the find-highlight rectangle for every match that
// intersects this line. Each match is clipped to the line the same way a
// selection is, so a match spanning a newline highlights on each line it
// touches.
func (rd rowDraw) drawFindMatches(gtx layout.Context, index int, lineText string) {
	for _, m := range rd.findMatches {
		if index < m.Start.Line || index > m.End.Line {
			continue
		}
		startCol := 0
		if index == m.Start.Line {
			startCol = m.Start.Column
		}
		endByte := len(lineText)
		extendEOL := index < m.End.Line
		if index == m.End.Line {
			endByte = m.End.Column
		}

		startVis := VisualColumn(lineText, startCol, rd.tabWidth)
		endVis := VisualColumn(lineText, endByte, rd.tabWidth)
		x0 := rd.gutterWidth + startVis*rd.cell
		x1 := rd.gutterWidth + endVis*rd.cell
		if extendEOL {
			x1 += rd.cell
		}
		if x1 <= x0 {
			if !extendEOL {
				continue
			}
			x1 = x0 + rd.cell
		}

		stack := op.Offset(image.Pt(x0, 0)).Push(gtx.Ops)
		paint.FillShape(gtx.Ops, rd.theme.FindHighlight, clip.Rect{Max: image.Point{X: x1 - x0, Y: rd.lineHeight}}.Op())
		stack.Pop()
	}
}

// drawDiagnostics draws a thin underline beneath every diagnostic span that
// intersects this line. Each diagnostic is clipped to the line the same way a
// selection is, so a diagnostic spanning several lines underlines each line it
// touches. A straight 2px bar (rather than a wavy squiggle) is used: it reuses
// the proven per-line clip/offset math, stays crisp at small cell sizes, and
// avoids per-pixel path work on the hot per-row render path. The bar sits at
// the bottom edge of the row and is colored by severity.
func (rd rowDraw) drawDiagnostics(gtx layout.Context, index int, lineText string) {
	thickness := diagnosticUnderlinePx
	y := rd.lineHeight - thickness
	if y < 0 {
		y = 0
	}
	for _, d := range rd.diags {
		start := d.Range.Start
		end := d.Range.End
		if index < start.Line || index > end.Line {
			continue
		}
		startCol := 0
		if index == start.Line {
			startCol = start.Column
		}
		endByte := len(lineText)
		extendEOL := index < end.Line
		if index == end.Line {
			endByte = end.Column
		}

		startVis := VisualColumn(lineText, startCol, rd.tabWidth)
		endVis := VisualColumn(lineText, endByte, rd.tabWidth)
		x0 := rd.gutterWidth + startVis*rd.cell
		x1 := rd.gutterWidth + endVis*rd.cell
		if extendEOL {
			x1 += rd.cell
		}
		if x1 <= x0 {
			// Zero-width or empty-line diagnostic: show at least one cell so
			// the problem is still visible where it was reported.
			x1 = x0 + rd.cell
		}

		stack := op.Offset(image.Pt(x0, y)).Push(gtx.Ops)
		paint.FillShape(gtx.Ops, rd.theme.DiagnosticColor(d.Severity),
			clip.Rect{Max: image.Point{X: x1 - x0, Y: thickness}}.Op())
		stack.Pop()
	}
}

// drawSegments draws each colored segment of the line at its absolute X,
// expanding tabs to spaces while tracking the running visual column so tab
// stops line up with VisualColumn.
func (rd rowDraw) drawSegments(gtx layout.Context, index int, lineText string) {
	if lineText == "" {
		return
	}
	var segments []Segment
	if rd.spans != nil && index < len(rd.spans) {
		segments = AssembleLine(lineText, rd.spans[index])
	} else {
		segments = AssembleLine(lineText, nil)
	}

	byteStart := 0
	for _, seg := range segments {
		visCol := VisualColumn(lineText, byteStart, rd.tabWidth)
		x := rd.gutterWidth + visCol*rd.cell
		expanded := expandTabs(seg.Text, visCol, rd.tabWidth)

		stack := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
		label := material.Label(rd.theme.Theme, rd.theme.Theme.TextSize, expanded)
		label.Font = rd.theme.MonoFont()
		label.Color = rd.theme.ColorForKind(seg.Kind)
		label.MaxLines = 1
		segGtx := gtx
		segGtx.Constraints.Min = image.Point{}
		segGtx.Constraints.Max = image.Point{X: len(expanded)*rd.cell + rd.cell, Y: rd.lineHeight}
		label.Layout(segGtx)
		stack.Pop()

		byteStart += len(seg.Text)
	}
}

// drawCaret draws the caret bar at the cursor's visual column.
func (rd rowDraw) drawCaret(gtx layout.Context, lineText string) {
	visCol := VisualColumn(lineText, rd.cursorCol, rd.tabWidth)
	x := rd.gutterWidth + visCol*rd.cell
	stack := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
	paint.FillShape(gtx.Ops, rd.theme.Caret, clip.Rect{Max: image.Point{X: caretWidthPx, Y: rd.lineHeight}}.Op())
	stack.Pop()
}

// expandTabs replaces each tab in text with enough spaces to reach the next
// tab stop, given the visual column at which text begins. Non-tab runes count
// as one cell each.
func expandTabs(text string, startCol, tabWidth int) string {
	if !strings.ContainsRune(text, '\t') {
		return text
	}
	if tabWidth <= 0 {
		tabWidth = DefaultTabWidth
	}
	var b strings.Builder
	col := startCol
	for _, r := range text {
		if r == '\t' {
			n := tabWidth - (col % tabWidth)
			for i := 0; i < n; i++ {
				b.WriteByte(' ')
			}
			col += n
			continue
		}
		b.WriteRune(r)
		col++
	}
	return b.String()
}

// editorTabWidth reads the configured tab size, falling back to the default
// when unset or non-positive.
func editorTabWidth(wb *Workbench) int {
	size := wb.Session().Config().Editor.TabSize
	if size <= 0 {
		return DefaultTabWidth
	}
	return size
}

// itoa renders a non-negative int in base 10 without allocating via fmt. It
// is used on the hot per-line path for line numbers.
func itoa(n int) string {
	if n <= 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
