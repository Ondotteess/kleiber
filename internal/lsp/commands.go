package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/Ondotteess/kleiber/internal/commands"
	"github.com/Ondotteess/kleiber/internal/editor"
)

const (
	// CommandFormatBuffer formats an already-open editor buffer through
	// the LSP bridge without saving it.
	CommandFormatBuffer = "lsp.formatBuffer"

	// CommandFormatAndSaveBuffer formats an already-open editor buffer
	// through the LSP bridge, then saves it to disk.
	CommandFormatAndSaveBuffer = "lsp.formatAndSaveBuffer"
)

var (
	// ErrCommandDispatcherNil is returned when command registration is
	// attempted without a dispatcher.
	ErrCommandDispatcherNil = errors.New("lsp: command dispatcher is nil")

	// ErrCommandBridgeNil is returned when command registration is
	// attempted without an LSP bridge.
	ErrCommandBridgeNil = errors.New("lsp: command bridge is nil")

	// ErrCommandMissingArg is returned when a command argument is required
	// but absent.
	ErrCommandMissingArg = errors.New("lsp: command missing argument")

	// ErrCommandInvalidArg is returned when a command argument has the
	// wrong type or value.
	ErrCommandInvalidArg = errors.New("lsp: command invalid argument")
)

// RegisterBridgeCommands registers LSP-backed editor commands with d.
func RegisterBridgeCommands(d *commands.Dispatcher, bridge *Bridge) error {
	if d == nil {
		return ErrCommandDispatcherNil
	}
	if bridge == nil {
		return ErrCommandBridgeNil
	}
	if err := d.Register(commands.Func{
		NameStr:        CommandFormatBuffer,
		DescriptionStr: "Format buffer with gopls",
		Fn: func(ctx context.Context, args map[string]any) error {
			id, opts, err := formatCommandArgs(args)
			if err != nil {
				return err
			}
			_, err = bridge.FormatBuffer(ctx, id, opts)
			return err
		},
	}); err != nil {
		return err
	}
	return d.Register(commands.Func{
		NameStr:        CommandFormatAndSaveBuffer,
		DescriptionStr: "Format buffer with gopls and save it",
		Fn: func(ctx context.Context, args map[string]any) error {
			id, opts, err := formatCommandArgs(args)
			if err != nil {
				return err
			}
			_, err = bridge.FormatAndSaveBuffer(ctx, id, opts)
			return err
		},
	})
}

func formatCommandArgs(args map[string]any) (editor.BufferID, FormattingOptions, error) {
	id, err := bufferIDArg(args, "bufferID")
	if err != nil {
		return 0, FormattingOptions{}, err
	}
	tabSize, err := optionalPositiveIntArg(args, "tabSize", 4)
	if err != nil {
		return 0, FormattingOptions{}, err
	}
	insertSpaces, err := optionalBoolArg(args, "insertSpaces", false)
	if err != nil {
		return 0, FormattingOptions{}, err
	}
	return id, FormattingOptions{TabSize: tabSize, InsertSpaces: insertSpaces}, nil
}

func bufferIDArg(args map[string]any, name string) (editor.BufferID, error) {
	if args == nil {
		return 0, fmt.Errorf("%w: %s", ErrCommandMissingArg, name)
	}
	raw, ok := args[name]
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrCommandMissingArg, name)
	}
	switch v := raw.(type) {
	case editor.BufferID:
		if v <= 0 {
			return 0, fmt.Errorf("%w: %s=%v", ErrCommandInvalidArg, name, v)
		}
		return v, nil
	case int:
		if v <= 0 {
			return 0, fmt.Errorf("%w: %s=%v", ErrCommandInvalidArg, name, v)
		}
		return editor.BufferID(v), nil
	case int64:
		if v <= 0 {
			return 0, fmt.Errorf("%w: %s=%v", ErrCommandInvalidArg, name, v)
		}
		return editor.BufferID(v), nil
	case float64:
		n, err := positiveInt64FromFloat(v, name)
		if err != nil {
			return 0, err
		}
		return editor.BufferID(n), nil
	case json.Number:
		n, err := positiveInt64FromNumber(v, name)
		if err != nil {
			return 0, err
		}
		return editor.BufferID(n), nil
	default:
		return 0, fmt.Errorf("%w: %s has type %T", ErrCommandInvalidArg, name, raw)
	}
}

func optionalPositiveIntArg(args map[string]any, name string, fallback int) (int, error) {
	raw, ok := args[name]
	if !ok {
		return fallback, nil
	}
	switch v := raw.(type) {
	case int:
		if v <= 0 {
			return 0, fmt.Errorf("%w: %s=%v", ErrCommandInvalidArg, name, v)
		}
		return v, nil
	case int64:
		return positiveIntFromInt64(v, name)
	case float64:
		n, err := positiveInt64FromFloat(v, name)
		if err != nil {
			return 0, err
		}
		return positiveIntFromInt64(n, name)
	case json.Number:
		n, err := positiveInt64FromNumber(v, name)
		if err != nil {
			return 0, err
		}
		return positiveIntFromInt64(n, name)
	default:
		return 0, fmt.Errorf("%w: %s has type %T", ErrCommandInvalidArg, name, raw)
	}
}

func optionalBoolArg(args map[string]any, name string, fallback bool) (bool, error) {
	raw, ok := args[name]
	if !ok {
		return fallback, nil
	}
	v, ok := raw.(bool)
	if !ok {
		return false, fmt.Errorf("%w: %s has type %T", ErrCommandInvalidArg, name, raw)
	}
	return v, nil
}

func positiveInt64FromFloat(v float64, name string) (int64, error) {
	if v <= 0 || math.Trunc(v) != v {
		return 0, fmt.Errorf("%w: %s=%v", ErrCommandInvalidArg, name, v)
	}
	n, err := strconv.ParseInt(strconv.FormatFloat(v, 'f', 0, 64), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %s=%v", ErrCommandInvalidArg, name, v)
	}
	return n, nil
}

func positiveInt64FromNumber(v json.Number, name string) (int64, error) {
	n, err := v.Int64()
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%w: %s=%v", ErrCommandInvalidArg, name, v)
	}
	return n, nil
}

func positiveIntFromInt64(n int64, name string) (int, error) {
	if n <= 0 || n > int64(int(^uint(0)>>1)) {
		return 0, fmt.Errorf("%w: %s=%v", ErrCommandInvalidArg, name, n)
	}
	return int(n), nil
}
