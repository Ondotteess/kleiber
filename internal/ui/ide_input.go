//go:build gio

package ui

import (
	"context"
	"io"
	"strings"
	"unicode/utf8"

	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/transfer"
	"gioui.org/layout"
	"gioui.org/op/clip"

	"github.com/Ondotteess/kleiber/internal/editor"
)

// editorTextMIME is the clipboard/transfer MIME type used for copy, cut and
// paste of editor text. It matches the type widget.Editor uses so the IDE
// interoperates with Gio's own text widgets.
const editorTextMIME = "application/text"

// editorInputTag is the event tag identifying the editor as a focus target.
// A distinct unexported type keeps the tag from colliding with any other tag
// routed through the same op list.
type editorInputTag struct{}

// registerInput marks area as the editor's focus target and installs the
// focus/transfer op needed to route keys and paste data to it. Mouse
// gestures are registered separately by registerMouse. It must be called
// while area (the editor's clip rectangle) is the active clip.
func (e *IDEEditor) registerInput(gtx layout.Context, area clip.Rect) {
	defer area.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, e.inputTag())
}

// inputTag returns the stable focus/pointer tag for this editor.
func (e *IDEEditor) inputTag() editorInputTag { return editorInputTag{} }

// handleInput consumes all pending editor key and transfer events for the
// frame and applies each to the active view or buffer. It returns true when
// any event mutated workbench state (buffer text or selection) so the caller
// can invalidate the window. Clipboard round-trips also report true so the
// resulting frame is scheduled.
//
// handleInput is a no-op unless the editor holds focus, except that it always
// processes the transfer.DataEvent that completes a paste (which may arrive
// while unfocused). Mouse input, including the press that focuses the
// editor, is handled by processMouse.
func (e *IDEEditor) handleInput(gtx layout.Context, wb *Workbench) bool {
	changed := false
	for {
		ev, ok := gtx.Event(e.inputFilters()...)
		if !ok {
			return changed
		}
		switch ev := ev.(type) {
		case key.EditEvent:
			// Typed text (including multi-byte input) replaces the selection.
			// Plain printable keys arrive here (not as key.Event), so this is
			// where typing dismisses an open LSP popup: the stale suggestions
			// no longer match once the buffer changes.
			if e.dismissLSPPopupsOnEdit() {
				changed = true
			}
			if view, err := wb.ActiveView(); err == nil && view != nil {
				if _, err := view.InsertText(ev.Text); err == nil {
					changed = true
				}
			}
		case transfer.DataEvent:
			// Completion of a paste initiated by Ctrl+V.
			if e.applyPaste(wb, ev) {
				changed = true
			}
		case key.Event:
			if ev.State != key.Press {
				continue
			}
			// LSP popups get first refusal on each key so their navigation
			// keys (arrows, Enter/Tab, Esc) act on the popup instead of the
			// buffer. A consumed key is fully handled here; an unconsumed key
			// may still have closed a popup (e.g. typing while it is open)
			// before falling through to normal editing.
			consumed, ch := e.handleLSPKey(gtx, wb, ev)
			if ch {
				changed = true
			}
			if consumed {
				continue
			}
			if e.applyKey(gtx, wb, ev) {
				changed = true
			}
		}
	}
}

// inputFilters returns the event filters the editor listens for: focus and
// text-edit events targeting its tag, the paste transfer target, and the
// full set of key filters for movement and editing shortcuts.
func (e *IDEEditor) inputFilters() []event.Filter {
	tag := e.inputTag()
	return []event.Filter{
		key.FocusFilter{Target: tag},
		transfer.TargetFilter{Target: tag, Type: editorTextMIME},

		key.Filter{Focus: tag, Name: key.NameReturn, Optional: key.ModShift},
		key.Filter{Focus: tag, Name: key.NameEnter, Optional: key.ModShift},
		key.Filter{Focus: tag, Name: key.NameTab, Optional: key.ModShift},
		key.Filter{Focus: tag, Name: key.NameDeleteBackward, Optional: key.ModShortcut | key.ModShift},
		key.Filter{Focus: tag, Name: key.NameDeleteForward, Optional: key.ModShortcut | key.ModShift},

		key.Filter{Focus: tag, Name: key.NameLeftArrow, Optional: key.ModShift},
		key.Filter{Focus: tag, Name: key.NameRightArrow, Optional: key.ModShift},
		key.Filter{Focus: tag, Name: key.NameUpArrow, Optional: key.ModShift},
		key.Filter{Focus: tag, Name: key.NameDownArrow, Optional: key.ModShift},
		key.Filter{Focus: tag, Name: key.NameHome, Optional: key.ModShift},
		key.Filter{Focus: tag, Name: key.NameEnd, Optional: key.ModShift},

		key.Filter{Focus: tag, Name: "A", Required: key.ModShortcut},
		key.Filter{Focus: tag, Name: "C", Required: key.ModShortcut},
		key.Filter{Focus: tag, Name: "X", Required: key.ModShortcut},
		key.Filter{Focus: tag, Name: "V", Required: key.ModShortcut},
		key.Filter{Focus: tag, Name: "Z", Required: key.ModShortcut, Optional: key.ModShift},
		key.Filter{Focus: tag, Name: "Y", Required: key.ModShortcut},
		key.Filter{Focus: tag, Name: "S", Required: key.ModShortcut},

		// LSP feature triggers, gated on editor focus by the tag: Ctrl+Space
		// completion, F1 hover, F12 go-to-definition — with Shift optional so
		// Shift+F12 (find references) arrives through the same filter and the
		// handler branches on the modifier — and Shift+Alt+F format document.
		// Escape closes any open popup, then the find bar, then the
		// references panel (see handleLSPKey for the priority order).
		key.Filter{Focus: tag, Name: key.NameSpace, Required: key.ModShortcut},
		key.Filter{Focus: tag, Name: key.NameF1},
		key.Filter{Focus: tag, Name: key.NameF12, Optional: key.ModShift},
		key.Filter{Focus: tag, Name: "F", Required: key.ModShift | key.ModAlt},
		key.Filter{Focus: tag, Name: key.NameEscape},
	}
}

// applyKey dispatches a single pressed key event to the matching edit or
// movement action. It returns true when the action mutated buffer or
// selection state (or scheduled a clipboard read that will).
func (e *IDEEditor) applyKey(gtx layout.Context, wb *Workbench, ke key.Event) bool {
	view, err := wb.ActiveView()
	if err != nil || view == nil {
		return false
	}
	buf := view.Buffer()
	if buf == nil {
		return false
	}
	shift := ke.Modifiers.Contain(key.ModShift)
	shortcut := ke.Modifiers.Contain(key.ModShortcut)

	if shortcut {
		return e.applyShortcut(gtx, wb, view, buf, ke, shift)
	}

	switch ke.Name {
	case key.NameReturn, key.NameEnter:
		return insertNewline(view, buf)
	case key.NameTab:
		if shift {
			// Shift+Tab outdent is intentionally not implemented.
			return false
		}
		_, err := view.InsertText("\t")
		return err == nil
	case key.NameDeleteBackward:
		_, err := view.Backspace()
		return err == nil
	case key.NameDeleteForward:
		return deleteForward(view, buf)
	case key.NameLeftArrow:
		return moveHorizontal(view, buf, -1, shift)
	case key.NameRightArrow:
		return moveHorizontal(view, buf, +1, shift)
	case key.NameUpArrow:
		return moveVertical(view, -1, shift)
	case key.NameDownArrow:
		return moveVertical(view, +1, shift)
	case key.NameHome:
		return moveLineEdge(view, buf, false, shift)
	case key.NameEnd:
		return moveLineEdge(view, buf, true, shift)
	}
	return false
}

// applyShortcut handles the platform-shortcut (Ctrl/Cmd) key combinations:
// select-all, clipboard copy/cut/paste, undo/redo and save.
func (e *IDEEditor) applyShortcut(gtx layout.Context, wb *Workbench, view *editor.View, buf *editor.Buffer, ke key.Event, shift bool) bool {
	switch ke.Name {
	case "A":
		return selectAll(view, buf)
	case "C":
		writeClipboard(gtx, view.SelectedText())
		return false
	case "X":
		text := view.SelectedText()
		if text == "" {
			return false
		}
		writeClipboard(gtx, text)
		_, err := view.Delete()
		return err == nil
	case "V":
		gtx.Execute(clipboard.ReadCmd{Tag: e.inputTag()})
		return false
	case "Z":
		if shift {
			_, ok := buf.Redo()
			return ok
		}
		_, ok := buf.Undo()
		return ok
	case "Y":
		_, ok := buf.Redo()
		return ok
	case "S":
		if tab, ok := wb.ActiveTab(); ok {
			// Save errors are non-fatal to editing; ignore them here.
			_ = wb.Session().SaveBuffer(context.Background(), tab.BufferID)
		}
		return false
	}
	return false
}

// applyPaste inserts the text carried by a paste transfer event into the
// active view, replacing any selection. It returns true when text was
// inserted.
func (e *IDEEditor) applyPaste(wb *Workbench, ev transfer.DataEvent) bool {
	view, err := wb.ActiveView()
	if err != nil || view == nil {
		return false
	}
	reader := ev.Open()
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil || len(data) == 0 {
		return false
	}
	if _, err := view.InsertText(string(data)); err != nil {
		return false
	}
	return true
}

// insertNewline inserts a line break followed by the auto-indent computed
// from the cursor's current line, matching Go-style block indentation.
func insertNewline(view *editor.View, buf *editor.Buffer) bool {
	cursor := view.Selection().Cursor
	prev, _ := buf.LineText(cursor.Line)
	indent := editor.AutoIndent(prev)
	if _, err := view.InsertText("\n" + indent); err != nil {
		return false
	}
	return true
}

// deleteForward deletes the selection when non-empty; otherwise it removes the
// single rune after the caret. At end-of-line it joins the following line by
// deleting the newline. It is a no-op at end-of-document.
func deleteForward(view *editor.View, buf *editor.Buffer) bool {
	sel := view.Selection()
	if !sel.IsEmpty() {
		_, err := view.Delete()
		return err == nil
	}
	cursor := sel.Cursor
	end, ok := positionAfterRune(buf, cursor)
	if !ok {
		return false
	}
	if err := view.SetSelection(editor.NewSelection(cursor, end)); err != nil {
		return false
	}
	if _, err := view.Delete(); err != nil {
		return false
	}
	return true
}

// positionAfterRune returns the position one rune after p, crossing to the
// start of the next line when p sits at end-of-line. The bool is false when p
// is already at end-of-document.
func positionAfterRune(buf *editor.Buffer, p editor.Position) (editor.Position, bool) {
	text, ok := buf.LineText(p.Line)
	if !ok {
		return editor.Position{}, false
	}
	if p.Column < len(text) {
		_, size := utf8.DecodeRuneInString(text[p.Column:])
		if size == 0 {
			return editor.Position{}, false
		}
		return editor.Position{Line: p.Line, Column: p.Column + size}, true
	}
	// End of line: join with the next line if one exists.
	if p.Line+1 < buf.Lines() {
		return editor.Position{Line: p.Line + 1, Column: 0}, true
	}
	return editor.Position{}, false
}

// moveHorizontal moves the caret one rune left (dir<0) or right (dir>0). With
// shift held it extends the selection to the rune-stepped target via
// MoveCursorTo; without shift it collapses to the target with MoveBy, which is
// itself rune-aware.
func moveHorizontal(view *editor.View, buf *editor.Buffer, dir int, shift bool) bool {
	if !shift {
		return view.MoveBy(0, dir) == nil
	}
	cursor := view.Selection().Cursor
	var target editor.Position
	var ok bool
	if dir < 0 {
		target, ok = positionBeforeRune(buf, cursor)
	} else {
		target, ok = positionAfterRune(buf, cursor)
	}
	if !ok {
		return false
	}
	return view.MoveCursorTo(target) == nil
}

// positionBeforeRune returns the position one rune before p, crossing to the
// end of the previous line when p sits at column 0. The bool is false when p
// is already at document start.
func positionBeforeRune(buf *editor.Buffer, p editor.Position) (editor.Position, bool) {
	if p.Column > 0 {
		text, ok := buf.LineText(p.Line)
		if ok && p.Column <= len(text) {
			if _, size := utf8.DecodeLastRuneInString(text[:p.Column]); size > 0 {
				return editor.Position{Line: p.Line, Column: p.Column - size}, true
			}
		}
		return editor.Position{Line: p.Line, Column: p.Column - 1}, true
	}
	if p.Line == 0 {
		return editor.Position{}, false
	}
	prev := p.Line - 1
	text, ok := buf.LineText(prev)
	if !ok {
		return editor.Position{}, false
	}
	return editor.Position{Line: prev, Column: len(text)}, true
}

// moveVertical moves the caret one line up (dir<0) or down (dir>0). Without
// shift it collapses via MoveBy (which keeps the byte column, snapped to the
// target line's nearest rune start and clamped to its length). With shift it
// extends the selection to the same target column on the adjacent line via
// MoveCursorTo, whose clamp keeps the column within the line; the column is
// preserved across the move so repeated Shift+Up/Down tracks a stable column.
func moveVertical(view *editor.View, dir int, shift bool) bool {
	if !shift {
		return view.MoveBy(dir, 0) == nil
	}
	cursor := view.Selection().Cursor
	target := editor.Position{Line: cursor.Line + dir, Column: cursor.Column}
	return view.MoveCursorTo(target) == nil
}

// moveLineEdge moves the caret to the start (end==false) or end (end==true) of
// its current line, extending the selection when shift is held.
func moveLineEdge(view *editor.View, buf *editor.Buffer, end, shift bool) bool {
	if !shift {
		if end {
			return view.MoveToLineEnd() == nil
		}
		return view.MoveToLineStart() == nil
	}
	cursor := view.Selection().Cursor
	col := 0
	if end {
		if text, ok := buf.LineText(cursor.Line); ok {
			col = len(text)
		}
	}
	return view.MoveCursorTo(editor.Position{Line: cursor.Line, Column: col}) == nil
}

// selectAll selects the entire document from (0,0) to the end offset.
func selectAll(view *editor.View, buf *editor.Buffer) bool {
	docEnd, err := buf.PositionAt(len(buf.Text()))
	if err != nil {
		return false
	}
	start := editor.Position{Line: 0, Column: 0}
	return view.SetSelection(editor.NewSelection(start, docEnd)) == nil
}

// writeClipboard puts s on the system clipboard as plain text. An empty
// string is not written so Ctrl+C with no selection leaves the clipboard
// untouched.
func writeClipboard(gtx layout.Context, s string) {
	if s == "" {
		return
	}
	gtx.Execute(clipboard.WriteCmd{
		Type: editorTextMIME,
		Data: io.NopCloser(strings.NewReader(s)),
	})
}
