package editor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/Ondotteess/kleiber/internal/commands"
)

const (
	// CommandOpenFile opens an on-disk file into the editor engine.
	CommandOpenFile = "editor.openFile"

	// CommandNewBuffer creates an untitled editor buffer.
	CommandNewBuffer = "editor.newBuffer"

	// CommandCloseBuffer closes an editor buffer.
	CommandCloseBuffer = "editor.closeBuffer"

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
)

var (
	// ErrCommandDispatcherNil is returned when command registration is
	// attempted without a dispatcher.
	ErrCommandDispatcherNil = errors.New("editor: command dispatcher is nil")

	// ErrCommandEngineNil is returned when command registration is attempted
	// without an editor engine.
	ErrCommandEngineNil = errors.New("editor: command engine is nil")

	// ErrCommandMissingArg is returned when a command argument is required but
	// absent.
	ErrCommandMissingArg = errors.New("editor: command missing argument")

	// ErrCommandInvalidArg is returned when a command argument has the wrong
	// type or value.
	ErrCommandInvalidArg = errors.New("editor: command invalid argument")
)

// RegisterCommands registers editor-owned commands with d. These commands only
// mutate editor state; query/state reads remain direct EditorEngine calls until
// the dispatcher grows a result API.
func RegisterCommands(d *commands.Dispatcher, engine *EditorEngine) error {
	if d == nil {
		return ErrCommandDispatcherNil
	}
	if engine == nil {
		return ErrCommandEngineNil
	}
	for _, cmd := range []commands.Command{
		commands.Func{
			NameStr:        CommandOpenFile,
			DescriptionStr: "Open file",
			Fn: func(ctx context.Context, args map[string]any) error {
				path, err := requiredStringArg(args, "path")
				if err != nil {
					return err
				}
				_, err = engine.Open(ctx, path)
				return err
			},
		},
		commands.Func{
			NameStr:        CommandNewBuffer,
			DescriptionStr: "Create untitled buffer",
			Fn: func(ctx context.Context, args map[string]any) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				text, err := optionalStringArg(args, "text", "")
				if err != nil {
					return err
				}
				engine.NewBuffer(text)
				return nil
			},
		},
		commands.Func{
			NameStr:        CommandCloseBuffer,
			DescriptionStr: "Close buffer",
			Fn: func(ctx context.Context, args map[string]any) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				id, err := bufferIDCommandArg(args, "bufferID")
				if err != nil {
					return err
				}
				return engine.Close(id)
			},
		},
		commands.Func{
			NameStr:        CommandSaveAsBuffer,
			DescriptionStr: "Save buffer as",
			Fn: func(ctx context.Context, args map[string]any) error {
				id, err := bufferIDCommandArg(args, "bufferID")
				if err != nil {
					return err
				}
				path, err := requiredStringArg(args, "path")
				if err != nil {
					return err
				}
				return engine.SaveAs(ctx, id, path)
			},
		},
		commands.Func{
			NameStr:        CommandNewView,
			DescriptionStr: "Create view",
			Fn: func(ctx context.Context, args map[string]any) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				id, err := bufferIDCommandArg(args, "bufferID")
				if err != nil {
					return err
				}
				_, err = engine.NewView(id)
				return err
			},
		},
		commands.Func{
			NameStr:        CommandCloseView,
			DescriptionStr: "Close view",
			Fn: func(ctx context.Context, args map[string]any) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				id, err := viewIDCommandArg(args, "viewID")
				if err != nil {
					return err
				}
				return engine.CloseView(id)
			},
		},
		commands.Func{
			NameStr:        CommandMoveCursor,
			DescriptionStr: "Move cursor",
			Fn: func(ctx context.Context, args map[string]any) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				vid, line, column, extend, err := moveCursorCommandArgs(args)
				if err != nil {
					return err
				}
				view, err := engine.View(vid)
				if err != nil {
					return err
				}
				pos := Position{Line: line, Column: column}
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
				if err := ctx.Err(); err != nil {
					return err
				}
				vid, err := viewIDCommandArg(args, "viewID")
				if err != nil {
					return err
				}
				text, err := requiredStringArg(args, "text")
				if err != nil {
					return err
				}
				view, err := engine.View(vid)
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
				if err := ctx.Err(); err != nil {
					return err
				}
				vid, err := viewIDCommandArg(args, "viewID")
				if err != nil {
					return err
				}
				view, err := engine.View(vid)
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
				if err := ctx.Err(); err != nil {
					return err
				}
				vid, err := viewIDCommandArg(args, "viewID")
				if err != nil {
					return err
				}
				view, err := engine.View(vid)
				if err != nil {
					return err
				}
				_, err = view.Delete()
				return err
			},
		},
	} {
		if err := d.Register(cmd); err != nil {
			return err
		}
	}
	return nil
}

func moveCursorCommandArgs(args map[string]any) (ViewID, int, int, bool, error) {
	vid, err := viewIDCommandArg(args, "viewID")
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

func bufferIDCommandArg(args map[string]any, name string) (BufferID, error) {
	raw, err := requiredArg(args, name)
	if err != nil {
		return 0, err
	}
	if id, ok := raw.(BufferID); ok {
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
	return BufferID(n), nil
}

func viewIDCommandArg(args map[string]any, name string) (ViewID, error) {
	raw, err := requiredArg(args, name)
	if err != nil {
		return 0, err
	}
	if id, ok := raw.(ViewID); ok {
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
	return ViewID(n), nil
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
