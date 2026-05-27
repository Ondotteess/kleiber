package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strconv"

	"github.com/Ondotteess/kleiber/internal/commands"
	"github.com/Ondotteess/kleiber/internal/editor"
	"github.com/Ondotteess/kleiber/internal/lsp"
)

const (
	// CommandOpenFile opens an on-disk file into the editor engine.
	CommandOpenFile = "editor.openFile"

	// CommandNewBuffer creates an untitled editor buffer.
	CommandNewBuffer = "editor.newBuffer"

	// CommandCloseBuffer closes an editor buffer.
	CommandCloseBuffer = "editor.closeBuffer"

	// CommandSaveBuffer saves an editor buffer through app-level policy.
	CommandSaveBuffer = "editor.saveBuffer"

	// CommandSaveAsBuffer writes a buffer to a caller-supplied path.
	CommandSaveAsBuffer = "editor.saveAsBuffer"

	// CommandNewView creates an editor view for a buffer.
	CommandNewView = "editor.newView"

	// CommandCloseView closes an editor view.
	CommandCloseView = "editor.closeView"

	// CommandMoveCursor moves a view cursor to an editor byte position.
	CommandMoveCursor = "editor.moveCursor"

	// CommandInsertText inserts text through a view at its current selection.
	CommandInsertText = "editor.insertText"

	// CommandBackspace deletes text before a view cursor or its selection.
	CommandBackspace = "editor.backspace"

	// CommandDeleteSelection deletes a view's current selection.
	CommandDeleteSelection = "editor.deleteSelection"

	// CommandProjectRefresh reloads project module/package metadata from disk.
	CommandProjectRefresh = "project.refresh"
)

var (
	// ErrCommandSessionNil is returned when command registration is attempted
	// without a session.
	ErrCommandSessionNil = errors.New("app: command session is nil")

	// ErrCommandDispatcherNil is returned when command registration is
	// attempted without a dispatcher.
	ErrCommandDispatcherNil = errors.New("app: command dispatcher is nil")

	// ErrCommandEditorNil is returned when editor command execution has no
	// editor engine available.
	ErrCommandEditorNil = errors.New("app: command editor is nil")

	// ErrCommandProjectNil is returned when project command execution has no
	// project attached to the session.
	ErrCommandProjectNil = errors.New("app: command project is nil")

	// ErrCommandMissingArg is returned when a command argument is required but
	// absent.
	ErrCommandMissingArg = errors.New("app: command missing argument")

	// ErrCommandInvalidArg is returned when a command argument has the wrong
	// type or value.
	ErrCommandInvalidArg = errors.New("app: command invalid argument")
)

// RegisterCommands registers app-owned mutation commands with the session's
// dispatcher. State/query reads remain typed APIs until the dispatcher grows a
// result-value contract.
func (s *Session) RegisterCommands() error {
	if s == nil {
		return ErrCommandSessionNil
	}
	if s.dispatcher == nil {
		return ErrCommandDispatcherNil
	}
	for _, cmd := range []commands.Command{
		commands.Func{
			NameStr:        CommandOpenFile,
			DescriptionStr: "Open file",
			Fn: func(ctx context.Context, args map[string]any) error {
				if s.editor == nil {
					return ErrCommandEditorNil
				}
				path, err := requiredStringArg(args, "path")
				if err != nil {
					return err
				}
				_, err = s.editor.Open(ctx, path)
				return err
			},
		},
		commands.Func{
			NameStr:        CommandNewBuffer,
			DescriptionStr: "Create untitled buffer",
			Fn: func(ctx context.Context, args map[string]any) error {
				if s.editor == nil {
					return ErrCommandEditorNil
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				text, err := optionalStringArg(args, "text", "")
				if err != nil {
					return err
				}
				s.editor.NewBuffer(text)
				return nil
			},
		},
		commands.Func{
			NameStr:        CommandCloseBuffer,
			DescriptionStr: "Close buffer",
			Fn: func(ctx context.Context, args map[string]any) error {
				if s.editor == nil {
					return ErrCommandEditorNil
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				id, err := bufferIDArg(args, "bufferID")
				if err != nil {
					return err
				}
				return s.editor.Close(id)
			},
		},
		commands.Func{
			NameStr:        CommandSaveBuffer,
			DescriptionStr: "Save buffer",
			Fn: func(ctx context.Context, args map[string]any) error {
				id, err := bufferIDArg(args, "bufferID")
				if err != nil {
					return err
				}
				return s.SaveBuffer(ctx, id)
			},
		},
		commands.Func{
			NameStr:        CommandSaveAsBuffer,
			DescriptionStr: "Save buffer as",
			Fn: func(ctx context.Context, args map[string]any) error {
				if s.editor == nil {
					return ErrCommandEditorNil
				}
				id, err := bufferIDArg(args, "bufferID")
				if err != nil {
					return err
				}
				path, err := requiredStringArg(args, "path")
				if err != nil {
					return err
				}
				return s.editor.SaveAs(ctx, id, path)
			},
		},
		commands.Func{
			NameStr:        CommandNewView,
			DescriptionStr: "Create view",
			Fn: func(ctx context.Context, args map[string]any) error {
				if s.editor == nil {
					return ErrCommandEditorNil
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				id, err := bufferIDArg(args, "bufferID")
				if err != nil {
					return err
				}
				_, err = s.editor.NewView(id)
				return err
			},
		},
		commands.Func{
			NameStr:        CommandCloseView,
			DescriptionStr: "Close view",
			Fn: func(ctx context.Context, args map[string]any) error {
				if s.editor == nil {
					return ErrCommandEditorNil
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				id, err := viewIDArg(args, "viewID")
				if err != nil {
					return err
				}
				return s.editor.CloseView(id)
			},
		},
		commands.Func{
			NameStr:        CommandMoveCursor,
			DescriptionStr: "Move cursor",
			Fn: func(ctx context.Context, args map[string]any) error {
				if s.editor == nil {
					return ErrCommandEditorNil
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				vid, line, column, extend, err := moveCursorArgs(args)
				if err != nil {
					return err
				}
				view, err := s.editor.View(vid)
				if err != nil {
					return err
				}
				pos := editor.Position{Line: line, Column: column}
				if extend {
					return view.MoveCursorTo(pos)
				}
				return view.MoveTo(pos)
			},
		},
		commands.Func{
			NameStr:        CommandInsertText,
			DescriptionStr: "Insert text",
			Fn: func(ctx context.Context, args map[string]any) error {
				if s.editor == nil {
					return ErrCommandEditorNil
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				vid, err := viewIDArg(args, "viewID")
				if err != nil {
					return err
				}
				text, err := requiredStringArg(args, "text")
				if err != nil {
					return err
				}
				view, err := s.editor.View(vid)
				if err != nil {
					return err
				}
				_, err = view.InsertText(text)
				return err
			},
		},
		commands.Func{
			NameStr:        CommandBackspace,
			DescriptionStr: "Backspace",
			Fn: func(ctx context.Context, args map[string]any) error {
				if s.editor == nil {
					return ErrCommandEditorNil
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				vid, err := viewIDArg(args, "viewID")
				if err != nil {
					return err
				}
				view, err := s.editor.View(vid)
				if err != nil {
					return err
				}
				_, err = view.Backspace()
				return err
			},
		},
		commands.Func{
			NameStr:        CommandDeleteSelection,
			DescriptionStr: "Delete selection",
			Fn: func(ctx context.Context, args map[string]any) error {
				if s.editor == nil {
					return ErrCommandEditorNil
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				vid, err := viewIDArg(args, "viewID")
				if err != nil {
					return err
				}
				view, err := s.editor.View(vid)
				if err != nil {
					return err
				}
				_, err = view.Delete()
				return err
			},
		},
		commands.Func{
			NameStr:        CommandProjectRefresh,
			DescriptionStr: "Refresh project metadata",
			Fn: func(ctx context.Context, args map[string]any) error {
				if s.project == nil {
					return ErrCommandProjectNil
				}
				return s.project.Refresh(ctx)
			},
		},
	} {
		if err := s.dispatcher.Register(cmd); err != nil {
			return err
		}
	}
	return nil
}

// SaveBuffer saves id through app-level orchestration. The editor engine stays
// LSP-agnostic: format-on-save delegates to the optional formatter only for Go
// buffers, and untracked documents fall back to plain save.
func (s *Session) SaveBuffer(ctx context.Context, id editor.BufferID) error {
	if s == nil || s.editor == nil {
		return ErrCommandEditorNil
	}
	if !s.cfg.Editor.FormatOnSave {
		return s.editor.Save(ctx, id)
	}

	path, err := s.editor.Path(id)
	if err != nil {
		return err
	}
	if path == "" || !isGoFile(path) || s.formatter == nil {
		return s.editor.Save(ctx, id)
	}

	_, err = s.formatter.FormatAndSaveBuffer(ctx, id, lsp.FormattingOptions{
		TabSize:      s.cfg.Editor.TabSize,
		InsertSpaces: s.cfg.Editor.InsertSpaces,
	})
	if errors.Is(err, lsp.ErrBridgeDocumentNotTracked) {
		return s.editor.Save(ctx, id)
	}
	return err
}

func isGoFile(path string) bool {
	return filepath.Ext(path) == ".go"
}

func moveCursorArgs(args map[string]any) (editor.ViewID, int, int, bool, error) {
	vid, err := viewIDArg(args, "viewID")
	if err != nil {
		return 0, 0, 0, false, err
	}
	line, err := nonNegativeIntArg(args, "line")
	if err != nil {
		return 0, 0, 0, false, err
	}
	column, err := nonNegativeIntArg(args, "column")
	if err != nil {
		return 0, 0, 0, false, err
	}
	extend, err := optionalBoolArg(args, "extendSelection", false)
	if err != nil {
		return 0, 0, 0, false, err
	}
	return vid, line, column, extend, nil
}

func bufferIDArg(args map[string]any, name string) (editor.BufferID, error) {
	raw, err := requiredArg(args, name)
	if err != nil {
		return 0, err
	}
	if id, ok := raw.(editor.BufferID); ok {
		if id <= 0 {
			return 0, fmt.Errorf("%w: %s=%d", ErrCommandInvalidArg, name, id)
		}
		return id, nil
	}
	n, err := numericInt64(raw, name)
	if err != nil {
		return 0, err
	}
	if n <= 0 {
		return 0, fmt.Errorf("%w: %s=%d", ErrCommandInvalidArg, name, n)
	}
	return editor.BufferID(n), nil
}

func viewIDArg(args map[string]any, name string) (editor.ViewID, error) {
	raw, err := requiredArg(args, name)
	if err != nil {
		return 0, err
	}
	if id, ok := raw.(editor.ViewID); ok {
		if id <= 0 {
			return 0, fmt.Errorf("%w: %s=%d", ErrCommandInvalidArg, name, id)
		}
		return id, nil
	}
	n, err := numericInt64(raw, name)
	if err != nil {
		return 0, err
	}
	if n <= 0 {
		return 0, fmt.Errorf("%w: %s=%d", ErrCommandInvalidArg, name, n)
	}
	return editor.ViewID(n), nil
}

func nonNegativeIntArg(args map[string]any, name string) (int, error) {
	raw, err := requiredArg(args, name)
	if err != nil {
		return 0, err
	}
	n, err := numericInt64(raw, name)
	if err != nil {
		return 0, err
	}
	if n < 0 || n > int64(int(^uint(0)>>1)) {
		return 0, fmt.Errorf("%w: %s=%d", ErrCommandInvalidArg, name, n)
	}
	return int(n), nil
}

func requiredArg(args map[string]any, name string) (any, error) {
	if args == nil {
		return nil, fmt.Errorf("%w: %s", ErrCommandMissingArg, name)
	}
	raw, ok := args[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrCommandMissingArg, name)
	}
	return raw, nil
}

func numericInt64(raw any, name string) (int64, error) {
	switch v := raw.(type) {
	case int:
		return int64(v), nil
	case int64:
		return v, nil
	case float64:
		if v > float64(math.MaxInt64) || v < float64(math.MinInt64) || math.Trunc(v) != v {
			return 0, fmt.Errorf("%w: %s=%v", ErrCommandInvalidArg, name, v)
		}
		n, err := strconv.ParseInt(strconv.FormatFloat(v, 'f', 0, 64), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%w: %s=%v", ErrCommandInvalidArg, name, v)
		}
		return n, nil
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0, fmt.Errorf("%w: %s=%v", ErrCommandInvalidArg, name, v)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("%w: %s has type %T", ErrCommandInvalidArg, name, raw)
	}
}

func requiredStringArg(args map[string]any, name string) (string, error) {
	if args == nil {
		return "", fmt.Errorf("%w: %s", ErrCommandMissingArg, name)
	}
	raw, ok := args[name]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrCommandMissingArg, name)
	}
	value, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%w: %s has type %T", ErrCommandInvalidArg, name, raw)
	}
	if value == "" {
		return "", fmt.Errorf("%w: %s is empty", ErrCommandInvalidArg, name)
	}
	return value, nil
}

func optionalStringArg(args map[string]any, name, fallback string) (string, error) {
	if args == nil {
		return fallback, nil
	}
	raw, ok := args[name]
	if !ok {
		return fallback, nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%w: %s has type %T", ErrCommandInvalidArg, name, raw)
	}
	return value, nil
}

func optionalBoolArg(args map[string]any, name string, fallback bool) (bool, error) {
	if args == nil {
		return fallback, nil
	}
	raw, ok := args[name]
	if !ok {
		return fallback, nil
	}
	value, ok := raw.(bool)
	if !ok {
		return false, fmt.Errorf("%w: %s has type %T", ErrCommandInvalidArg, name, raw)
	}
	return value, nil
}
