//go:build gio

package ui

import (
	"context"
	"image"
	"image/color"
	"strings"
	"sync"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/Ondotteess/kleiber/internal/terminal"
)

// terminalToolbarPadDp is the vertical padding around the terminal toolbar
// row, applied above and below the button strip.
const terminalToolbarPadDp = 4

// terminalInputTag is the focus/pointer target identifying the terminal grid.
// A distinct unexported type keeps the tag from colliding with the editor's
// own tag when both route events through the same op list.
type terminalInputTag struct{}

// IDETerminal is the embedded terminal panel: a toolbar of Go-command buttons
// over a live monospace color grid rendered from a terminal.Session. It routes
// keyboard input to the session when focused, so the terminal is interactive.
//
// The panel owns at most one session at a time. Starting a new one (via a
// toolbar button or the first Layout) closes the previous session first. A
// single goroutine per session forwards the session's coalesced screen-change
// signals to the window's invalidate hook and terminates when the session is
// closed.
//
// IDETerminal is safe for concurrent use: the session pointer is guarded by a
// mutex because the render loop reads it while Close (called from the window's
// destroy path) may clear it.
type IDETerminal struct {
	// mu guards session and the last-known grid dimensions, which the render
	// loop writes and Close reads.
	mu      sync.Mutex
	session *terminal.Session
	cols    int
	rows    int

	// invalidate schedules a window redraw from any goroutine. It is set once
	// via SetInvalidate after the window is created and is safe to call
	// concurrently (Gio's Window.Invalidate is). The per-session updates
	// goroutine calls it on every screen change. It may be nil before
	// SetInvalidate runs.
	invalidate func()

	// shell runs the platform default shell; goCmds holds one button per
	// ui.GoCommand, aligned with GoCommands() order.
	shell  widget.Clickable
	goCmds []*widget.Clickable
}

// NewIDETerminal constructs an empty terminal panel with no session. The first
// Layout (or the first toolbar click) starts a session.
func NewIDETerminal() *IDETerminal {
	cmds := GoCommands()
	buttons := make([]*widget.Clickable, len(cmds))
	for i := range buttons {
		buttons[i] = &widget.Clickable{}
	}
	return &IDETerminal{
		cols:   defaultTerminalCols,
		rows:   defaultTerminalRows,
		goCmds: buttons,
	}
}

// Default grid dimensions used before the first Layout has measured the panel.
const (
	defaultTerminalCols = 80
	defaultTerminalRows = 24
)

// SetInvalidate stores the window's redraw hook so the per-session updates
// goroutine can schedule a frame when the screen changes. It is called once,
// after the window is created; fn must be safe to call from any goroutine
// (Gio's Window.Invalidate is).
func (t *IDETerminal) SetInvalidate(fn func()) {
	t.invalidate = fn
}

// Layout draws the terminal panel for wb and processes its input for the
// frame: a toolbar row of buttons over the grid rendered from the current
// session's screen snapshot. On the first Layout with no session, and whenever
// a toolbar button is clicked, it (re)starts a session. When the measured grid
// size changes it resizes the running session so the child sees the new
// dimensions.
func (t *IDETerminal) Layout(gtx layout.Context, th *IDETheme, wb *Workbench) layout.Dimensions {
	// Toolbar first so a click this frame starts its session before the grid
	// reads the snapshot below.
	t.handleToolbar(gtx, wb)

	// Ensure a session exists so an initial toggle-on shows a shell.
	if t.current() == nil {
		t.startSession(wb, nil)
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return t.layoutToolbar(gtx, th)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return t.layoutGrid(gtx, th)
		}),
	)
}

// handleToolbar dispatches toolbar button clicks to session starts: the shell
// button starts the platform default shell, each Go-command button starts that
// command. Buttons run in the panel's command directory (module root of the
// active file).
func (t *IDETerminal) handleToolbar(gtx layout.Context, wb *Workbench) {
	if t.shell.Clicked(gtx) {
		t.startSession(wb, nil)
	}
	cmds := GoCommands()
	for i, btn := range t.goCmds {
		if i >= len(cmds) {
			break
		}
		if btn.Clicked(gtx) {
			t.startSession(wb, cmds[i].Args)
		}
	}
}

// layoutToolbar draws the toolbar strip: a "Shell" button followed by one
// button per Go command, laid out on a single row over the toolbar fill.
func (t *IDETerminal) layoutToolbar(gtx layout.Context, th *IDETheme) layout.Dimensions {
	cmds := GoCommands()
	pad := gtx.Metric.Dp(unit.Dp(terminalToolbarPadDp))
	height := th.LineHeight(gtx) + 2*pad + gtx.Metric.Dp(unit.Dp(8))
	width := gtx.Constraints.Max.X
	paint.FillShape(gtx.Ops, th.TerminalToolbar, clip.Rect{Max: image.Point{X: width, Y: height}}.Op())

	gtx.Constraints.Min.Y = height
	gtx.Constraints.Max.Y = height
	return layout.Inset{
		Left:   unit.Dp(6),
		Right:  unit.Dp(6),
		Top:    unit.Dp(terminalToolbarPadDp),
		Bottom: unit.Dp(terminalToolbarPadDp),
	}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		children := make([]layout.FlexChild, 0, 1+2*len(cmds))
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return t.button(gtx, th, &t.shell, "Shell")
		}))
		for i := range cmds {
			i := i
			children = append(children, layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout))
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return t.button(gtx, th, t.goCmds[i], cmds[i].Label)
			}))
		}
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
	})
}

// button draws one toolbar button: a padded label over a panel-colored fill
// that highlights while hovered or pressed.
func (t *IDETerminal) button(gtx layout.Context, th *IDETheme, btn *widget.Clickable, label string) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				size := image.Point{X: gtx.Constraints.Min.X, Y: gtx.Constraints.Max.Y}
				bg := th.Panel
				if btn.Hovered() || btn.Pressed() {
					bg = th.ActiveTab
				}
				paint.FillShape(gtx.Ops, bg, clip.Rect{Max: size}.Op())
				return layout.Dimensions{Size: size}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{
					Left:   unit.Dp(8),
					Right:  unit.Dp(8),
					Top:    unit.Dp(2),
					Bottom: unit.Dp(2),
				}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body2(th.Theme, label)
					lbl.Font = th.MonoFont()
					lbl.Color = th.Foreground
					lbl.MaxLines = 1
					return lbl.Layout(gtx)
				})
			}),
		)
	})
}

// layoutGrid registers grid input, resizes the session to the measured size,
// and draws the current session's screen snapshot. With no session it fills
// the area with the terminal background.
func (t *IDETerminal) layoutGrid(gtx layout.Context, th *IDETheme) layout.Dimensions {
	size := gtx.Constraints.Max
	paint.FillShape(gtx.Ops, th.TerminalBg, clip.Rect{Max: size}.Op())

	area := clip.Rect{Max: size}
	t.registerInput(gtx, area)
	t.handleInput(gtx)

	cellW := th.CellWidth(gtx)
	lineH := th.LineHeight(gtx)
	cols := size.X / cellW
	rows := size.Y / lineH
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	t.resizeIfChanged(cols, rows)

	if s := t.current(); s != nil {
		snap := s.Screen().Snapshot()
		t.drawSnapshot(gtx, th, snap, cellW, lineH)
	}
	return layout.Dimensions{Size: size}
}

// registerInput marks area as the terminal grid's focus and pointer target so
// clicks focus the grid and, once focused, keys route to it. It must be called
// while area is the active clip so pointer hits are bounded to the grid.
func (t *IDETerminal) registerInput(gtx layout.Context, area clip.Rect) {
	defer area.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, t.inputTag())
}

// inputTag returns the stable focus/pointer tag for the terminal grid.
func (t *IDETerminal) inputTag() terminalInputTag { return terminalInputTag{} }

// handleInput consumes the terminal's pending input events for the frame. A
// pointer press focuses the grid; while focused, typed text and named control
// keys are written to the session as the bytes a terminal expects. It is a
// no-op when no session is running.
func (t *IDETerminal) handleInput(gtx layout.Context) {
	for {
		ev, ok := gtx.Event(t.inputFilters()...)
		if !ok {
			return
		}
		switch ev := ev.(type) {
		case pointer.Event:
			if ev.Kind == pointer.Press {
				gtx.Execute(key.FocusCmd{Tag: t.inputTag()})
			}
		case key.EditEvent:
			t.writeString(ev.Text)
		case key.Event:
			if ev.State != key.Press {
				continue
			}
			if data := controlBytes(ev); len(data) > 0 {
				t.write(data)
			}
		}
	}
}

// inputFilters returns the event filters the terminal grid listens for: focus
// events and a pointer press targeting its tag, plus the named keys it maps to
// terminal control sequences. Printable text arrives as key.EditEvent (covered
// by the focus filter), so no per-letter key filters are needed except the
// Ctrl+letter range, which is registered as the whole alphabet with ModCtrl
// required.
func (t *IDETerminal) inputFilters() []event.Filter {
	tag := t.inputTag()
	filters := []event.Filter{
		key.FocusFilter{Target: tag},
		pointer.Filter{Target: tag, Kinds: pointer.Press},

		key.Filter{Focus: tag, Name: key.NameReturn},
		key.Filter{Focus: tag, Name: key.NameEnter},
		key.Filter{Focus: tag, Name: key.NameDeleteBackward},
		key.Filter{Focus: tag, Name: key.NameTab},
		key.Filter{Focus: tag, Name: key.NameEscape},
		key.Filter{Focus: tag, Name: key.NameUpArrow},
		key.Filter{Focus: tag, Name: key.NameDownArrow},
		key.Filter{Focus: tag, Name: key.NameLeftArrow},
		key.Filter{Focus: tag, Name: key.NameRightArrow},
		key.Filter{Focus: tag, Name: key.NameHome},
		key.Filter{Focus: tag, Name: key.NameEnd},
	}
	// Ctrl+letter (e.g. Ctrl+C interrupt) for every ASCII letter. ModCtrl is
	// required so a plain letter still arrives as text via key.EditEvent.
	for r := 'A'; r <= 'Z'; r++ {
		filters = append(filters, key.Filter{
			Focus:    tag,
			Name:     key.Name(string(r)),
			Required: key.ModCtrl,
			Optional: key.ModShift,
		})
	}
	return filters
}

// controlBytes maps a pressed named key (or Ctrl+letter) to the byte sequence
// a terminal application expects. It returns nil for keys it does not handle;
// printable text is delivered separately as key.EditEvent.
func controlBytes(ke key.Event) []byte {
	// Ctrl+letter takes precedence: it produces the C0 control code letter&0x1f
	// (Ctrl+C = 0x03 interrupt, Ctrl+D = 0x04 EOF, ...). ModShortcut is folded
	// in so the mapping also fires where the platform shortcut is Ctrl.
	if ke.Modifiers.Contain(key.ModCtrl) || ke.Modifiers.Contain(key.ModShortcut) {
		name := string(ke.Name)
		if len(name) == 1 {
			c := name[0]
			if c >= 'A' && c <= 'Z' {
				return []byte{c & 0x1f}
			}
		}
	}
	switch ke.Name {
	case key.NameReturn, key.NameEnter:
		return []byte("\r")
	case key.NameDeleteBackward:
		return []byte("\x7f")
	case key.NameTab:
		return []byte("\t")
	case key.NameEscape:
		return []byte("\x1b")
	case key.NameUpArrow:
		return []byte("\x1b[A")
	case key.NameDownArrow:
		return []byte("\x1b[B")
	case key.NameRightArrow:
		return []byte("\x1b[C")
	case key.NameLeftArrow:
		return []byte("\x1b[D")
	case key.NameHome:
		return []byte("\x1b[H")
	case key.NameEnd:
		return []byte("\x1b[F")
	}
	return nil
}

// drawSnapshot renders one screen snapshot into the grid: each cell's
// background (when non-default) is filled, each non-blank rune is drawn in its
// foreground color, and the cursor block is drawn when visible. Inverse cells
// swap foreground and background before color resolution. Runs of same-style
// cells are batched into one label to reduce label churn.
func (t *IDETerminal) drawSnapshot(gtx layout.Context, th *IDETheme, snap terminal.ScreenSnapshot, cellW, lineH int) {
	for y, row := range snap.Cells {
		yPx := y * lineH
		// First pass: backgrounds. Filled per contiguous run of equal color.
		x := 0
		for x < len(row) {
			bg := cellBG(row[x])
			if bg == (color.NRGBA{}) {
				x++
				continue
			}
			start := x
			for x < len(row) && cellBG(row[x]) == bg {
				x++
			}
			stack := op.Offset(image.Pt(start*cellW, yPx)).Push(gtx.Ops)
			paint.FillShape(gtx.Ops, bg, clip.Rect{Max: image.Point{X: (x - start) * cellW, Y: lineH}}.Op())
			stack.Pop()
		}
		// Second pass: foreground runes batched by equal foreground color.
		t.drawRowText(gtx, th, row, yPx, cellW, lineH)
	}

	if snap.CursorVisible && snap.CursorY >= 0 && snap.CursorY < len(snap.Cells) {
		t.drawCursor(gtx, th, snap, cellW, lineH)
	}
}

// drawRowText draws the runes of one row, batching a maximal run of cells that
// share a foreground color into a single label. Blank cells (rune 0) break a
// run and render as spaces inside it so column alignment is preserved.
func (t *IDETerminal) drawRowText(gtx layout.Context, th *IDETheme, row []terminal.Cell, yPx, cellW, lineH int) {
	x := 0
	for x < len(row) {
		if row[x].R == 0 {
			x++
			continue
		}
		fg := cellFG(row[x])
		start := x
		var b strings.Builder
		for x < len(row) && row[x].R != 0 && cellFG(row[x]) == fg {
			b.WriteRune(row[x].R)
			x++
		}
		text := b.String()
		stack := op.Offset(image.Pt(start*cellW, yPx)).Push(gtx.Ops)
		label := material.Label(th.Theme, th.Theme.TextSize, text)
		label.Font = th.MonoFont()
		label.Color = fg
		label.MaxLines = 1
		cellGtx := gtx
		cellGtx.Constraints.Min = image.Point{}
		cellGtx.Constraints.Max = image.Point{X: len(text)*cellW + cellW, Y: lineH}
		label.Layout(cellGtx)
		stack.Pop()
	}
}

// drawCursor draws a filled cursor block at the snapshot's cursor position,
// then redraws the covered rune (if any) in the cell's background color so it
// stays legible against the block.
func (t *IDETerminal) drawCursor(gtx layout.Context, th *IDETheme, snap terminal.ScreenSnapshot, cellW, lineH int) {
	cx, cy := snap.CursorX, snap.CursorY
	row := snap.Cells[cy]
	if cx < 0 || cx >= len(row) {
		return
	}
	x := cx * cellW
	y := cy * lineH
	cell := row[cx]

	stack := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
	paint.FillShape(gtx.Ops, cellFG(cell), clip.Rect{Max: image.Point{X: cellW, Y: lineH}}.Op())
	stack.Pop()

	if cell.R != 0 {
		stack = op.Offset(image.Pt(x, y)).Push(gtx.Ops)
		label := material.Label(th.Theme, th.Theme.TextSize, string(cell.R))
		label.Font = th.MonoFont()
		label.Color = cellBGResolved(cell)
		label.MaxLines = 1
		cellGtx := gtx
		cellGtx.Constraints.Min = image.Point{}
		cellGtx.Constraints.Max = image.Point{X: 2 * cellW, Y: lineH}
		label.Layout(cellGtx)
		stack.Pop()
	}
}

// cellBG returns the background color to fill behind a cell, or the zero NRGBA
// when the cell resolves to the default background (which the grid already
// painted, so no per-cell fill is needed). Inverse is applied.
func cellBG(c terminal.Cell) color.NRGBA {
	bg := c.BG
	if c.Inverse {
		bg = c.FG
	}
	if bg.Kind == terminal.ColorDefault {
		return color.NRGBA{}
	}
	return termColorToNRGBA(bg, false)
}

// cellBGResolved returns the cell's background color always resolved to a
// concrete NRGBA (default included), for drawing a rune over the cursor block.
func cellBGResolved(c terminal.Cell) color.NRGBA {
	bg := c.BG
	if c.Inverse {
		bg = c.FG
	}
	return termColorToNRGBA(bg, false)
}

// cellFG returns the foreground color a cell's rune is drawn in, applying
// Inverse (which swaps foreground and background before resolution).
func cellFG(c terminal.Cell) color.NRGBA {
	fg := c.FG
	if c.Inverse {
		fg = c.BG
	}
	return termColorToNRGBA(fg, true)
}

// resizeIfChanged resizes the running session (if any) to cols x rows when they
// differ from the last-known dimensions, and records the new size regardless so
// a later session starts at the current grid size.
func (t *IDETerminal) resizeIfChanged(cols, rows int) {
	t.mu.Lock()
	changed := cols != t.cols || rows != t.rows
	t.cols, t.rows = cols, rows
	s := t.session
	t.mu.Unlock()
	if changed && s != nil {
		// A resize failure (e.g. after the child exited) is non-fatal.
		_ = s.Resize(cols, rows)
	}
}

// current returns the running session, or nil when none is started.
func (t *IDETerminal) current() *terminal.Session {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.session
}

// writeString sends s to the running session as keyboard input. It is a no-op
// when s is empty or no session is running.
func (t *IDETerminal) writeString(s string) {
	if s == "" {
		return
	}
	t.write([]byte(s))
}

// write sends raw bytes to the running session as keyboard input. Write errors
// (e.g. the child already exited) are non-fatal and ignored.
func (t *IDETerminal) write(data []byte) {
	if s := t.current(); s != nil {
		_, _ = s.Write(data)
	}
}

// startSession closes any running session and starts a new one running command
// (nil command means the platform default shell) in the workbench's command
// directory at the current grid size. It starts one goroutine that forwards the
// session's screen-change signals to the invalidate hook and ends when the
// session is closed. Start failures leave the panel session-less but usable.
func (t *IDETerminal) startSession(wb *Workbench, command []string) {
	t.mu.Lock()
	prev := t.session
	t.session = nil
	cols, rows := t.cols, t.rows
	t.mu.Unlock()

	if prev != nil {
		_ = prev.Close()
	}

	s, err := terminal.StartSession(context.Background(), terminal.Options{
		Command: command,
		Dir:     CommandDir(wb),
		Cols:    cols,
		Rows:    rows,
	})
	if err != nil {
		// Keep the panel usable; the next toolbar click can retry.
		return
	}

	t.mu.Lock()
	t.session = s
	t.mu.Unlock()

	invalidate := t.invalidate
	if invalidate != nil {
		// One goroutine per session. Updates() is closed by Session.Close,
		// which ends the range and terminates the goroutine; the trailing
		// invalidate repaints the final (post-exit) screen state.
		go func() {
			for range s.Updates() {
				invalidate()
			}
			invalidate()
		}()
		invalidate()
	}
}

// Close terminates the running session, killing its child process, and clears
// the session pointer. It is idempotent and safe to call when no session is
// running. It must be called before the process exits (the Gio launcher exits
// via os.Exit, which skips deferred cleanup) so the shell or Go command is not
// orphaned.
func (t *IDETerminal) Close() {
	t.mu.Lock()
	s := t.session
	t.session = nil
	t.mu.Unlock()
	if s != nil {
		_ = s.Close()
	}
}

// termColorToNRGBA resolves a terminal color to an opaque NRGBA. fg selects the
// default when the color is ColorDefault: the light-gray default foreground or
// the dark default background. Color16 and Color256 index the xterm palette;
// ColorRGB is used verbatim.
func termColorToNRGBA(c terminal.Color, fg bool) color.NRGBA {
	switch c.Kind {
	case terminal.ColorDefault:
		if fg {
			return color.NRGBA{R: 0xd4, G: 0xd4, B: 0xd4, A: 0xff}
		}
		return color.NRGBA{R: 0x1e, G: 0x1e, B: 0x1e, A: 0xff}
	case terminal.Color16:
		return ansi16[c.Index&0x0f]
	case terminal.Color256:
		return xterm256(c.Index)
	case terminal.ColorRGB:
		return color.NRGBA{R: c.R, G: c.G, B: c.B, A: 0xff}
	default:
		if fg {
			return color.NRGBA{R: 0xd4, G: 0xd4, B: 0xd4, A: 0xff}
		}
		return color.NRGBA{R: 0x1e, G: 0x1e, B: 0x1e, A: 0xff}
	}
}

// ansi16 is the standard xterm 16-color palette: entries 0-7 are the normal
// colors, 8-15 their bright variants.
var ansi16 = [16]color.NRGBA{
	{R: 0x00, G: 0x00, B: 0x00, A: 0xff}, // 0 black
	{R: 0xcd, G: 0x00, B: 0x00, A: 0xff}, // 1 red
	{R: 0x00, G: 0xcd, B: 0x00, A: 0xff}, // 2 green
	{R: 0xcd, G: 0xcd, B: 0x00, A: 0xff}, // 3 yellow
	{R: 0x00, G: 0x00, B: 0xee, A: 0xff}, // 4 blue
	{R: 0xcd, G: 0x00, B: 0xcd, A: 0xff}, // 5 magenta
	{R: 0x00, G: 0xcd, B: 0xcd, A: 0xff}, // 6 cyan
	{R: 0xe5, G: 0xe5, B: 0xe5, A: 0xff}, // 7 white
	{R: 0x7f, G: 0x7f, B: 0x7f, A: 0xff}, // 8 bright black
	{R: 0xff, G: 0x00, B: 0x00, A: 0xff}, // 9 bright red
	{R: 0x00, G: 0xff, B: 0x00, A: 0xff}, // 10 bright green
	{R: 0xff, G: 0xff, B: 0x00, A: 0xff}, // 11 bright yellow
	{R: 0x5c, G: 0x5c, B: 0xff, A: 0xff}, // 12 bright blue
	{R: 0xff, G: 0x00, B: 0xff, A: 0xff}, // 13 bright magenta
	{R: 0x00, G: 0xff, B: 0xff, A: 0xff}, // 14 bright cyan
	{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, // 15 bright white
}

// xterm256 resolves an xterm 256-color index to an NRGBA: 0-15 reuse the ANSI
// palette, 16-231 form a 6x6x6 RGB cube, and 232-255 are a 24-step grayscale
// ramp.
func xterm256(index uint8) color.NRGBA {
	switch {
	case index < 16:
		return ansi16[index]
	case index < 232:
		n := int(index) - 16
		r := n / 36
		g := (n / 6) % 6
		b := n % 6
		return color.NRGBA{R: cubeLevel(r), G: cubeLevel(g), B: cubeLevel(b), A: 0xff}
	default:
		// 232-255: gray ramp from 8 to 238 in steps of 10.
		v := uint8(8 + 10*(int(index)-232))
		return color.NRGBA{R: v, G: v, B: v, A: 0xff}
	}
}

// cubeLevel maps a 0-5 color-cube coordinate to its xterm 8-bit channel value.
// Level 0 is 0; levels 1-5 are 95, 135, 175, 215, 255.
func cubeLevel(v int) uint8 {
	if v <= 0 {
		return 0
	}
	return uint8(55 + 40*v)
}
