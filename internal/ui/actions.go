package ui

import (
	"context"
	"errors"

	"github.com/Ondotteess/kleiber/internal/app"
	"github.com/Ondotteess/kleiber/internal/editor"
)

// ErrNilPresenter is returned when a controller is constructed without a
// presenter to refresh or notify.
var ErrNilPresenter = errors.New("ui: presenter is nil")

// ErrNilController is returned when a controller method is called on a nil
// receiver.
var ErrNilController = errors.New("ui: controller is nil")

// ControllerOptions configures NewController. It is intentionally empty while
// the controller is only a typed action facade over app commands.
type ControllerOptions struct{}

// Controller maps future UI intents to app-owned mutation commands. It does
// not render, import gioui, or mutate editor/project state directly.
type Controller struct {
	presenter *Presenter
	session   *app.Session
}

// NewController constructs a typed action facade for a Presenter and Session.
func NewController(presenter *Presenter, session *app.Session, opts ControllerOptions) (*Controller, error) {
	_ = opts
	if presenter == nil {
		return nil, ErrNilPresenter
	}
	if session == nil {
		return nil, ErrNilSession
	}
	return &Controller{presenter: presenter, session: session}, nil
}

// OpenFile dispatches editor.openFile.
func (c *Controller) OpenFile(ctx context.Context, path string) error {
	return c.dispatch(ctx, app.CommandOpenFile, map[string]any{"path": path})
}

// NewBuffer dispatches editor.newBuffer.
func (c *Controller) NewBuffer(ctx context.Context, text string) error {
	return c.dispatch(ctx, app.CommandNewBuffer, map[string]any{"text": text})
}

// CloseBuffer dispatches editor.closeBuffer.
func (c *Controller) CloseBuffer(ctx context.Context, id editor.BufferID) error {
	return c.dispatch(ctx, app.CommandCloseBuffer, map[string]any{"bufferID": id})
}

// SaveBuffer dispatches editor.saveBuffer.
func (c *Controller) SaveBuffer(ctx context.Context, id editor.BufferID) error {
	return c.dispatch(ctx, app.CommandSaveBuffer, map[string]any{"bufferID": id})
}

// SaveAsBuffer dispatches editor.saveAsBuffer.
func (c *Controller) SaveAsBuffer(ctx context.Context, id editor.BufferID, path string) error {
	return c.dispatch(ctx, app.CommandSaveAsBuffer, map[string]any{
		"bufferID": id,
		"path":     path,
	})
}

// NewView dispatches editor.newView.
func (c *Controller) NewView(ctx context.Context, bufferID editor.BufferID) error {
	return c.dispatch(ctx, app.CommandNewView, map[string]any{"bufferID": bufferID})
}

// CloseView dispatches editor.closeView.
func (c *Controller) CloseView(ctx context.Context, viewID editor.ViewID) error {
	return c.dispatch(ctx, app.CommandCloseView, map[string]any{"viewID": viewID})
}

// MoveCursor dispatches editor.moveCursor.
func (c *Controller) MoveCursor(ctx context.Context, viewID editor.ViewID, pos editor.Position, extendSelection bool) error {
	return c.dispatch(ctx, app.CommandMoveCursor, map[string]any{
		"viewID":          viewID,
		"line":            pos.Line,
		"column":          pos.Column,
		"extendSelection": extendSelection,
	})
}

// InsertText dispatches editor.insertText.
func (c *Controller) InsertText(ctx context.Context, viewID editor.ViewID, text string) error {
	return c.dispatch(ctx, app.CommandInsertText, map[string]any{
		"viewID": viewID,
		"text":   text,
	})
}

// Backspace dispatches editor.backspace.
func (c *Controller) Backspace(ctx context.Context, viewID editor.ViewID) error {
	return c.dispatch(ctx, app.CommandBackspace, map[string]any{"viewID": viewID})
}

// DeleteSelection dispatches editor.deleteSelection.
func (c *Controller) DeleteSelection(ctx context.Context, viewID editor.ViewID) error {
	return c.dispatch(ctx, app.CommandDeleteSelection, map[string]any{"viewID": viewID})
}

// RefreshProject dispatches project.refresh, then refreshes and signals the
// presenter because project metadata changes do not currently emit editor
// events.
func (c *Controller) RefreshProject(ctx context.Context) error {
	if err := c.dispatch(ctx, app.CommandProjectRefresh, nil); err != nil {
		return err
	}
	if err := c.presenter.Refresh(ctx); err != nil {
		return err
	}
	c.presenter.emitUpdate()
	return nil
}

func (c *Controller) dispatch(ctx context.Context, name string, args map[string]any) error {
	if c == nil {
		return ErrNilController
	}
	if c.session == nil {
		return ErrNilSession
	}
	return c.session.Dispatch(ctx, name, args)
}
